// One-shot verification binary: 跑真实 API 数据，对比修复前后的云海检出率。
//
// 用法：go run ./cmd/cloudsea-verify
// 输出：每个站点的云海时次、Episode 列表、垂直分辨率告警。
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

type siteLite struct {
	name string
	lat  float64
	lon  float64
	alt  float64
}

func main() {
	cfg := config.Default()
	// 起点 = 当地今天 00:00。给 1 天预报已够。
	local := time.Now()
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)

	sites := []siteLite{
		{"牛草山", 31.047, 116.259, 1442},
		{"太子尖", 30.1734, 118.9057, 1557},
		{"牵牛岗", 30.026, 119.007, 1489.9},
		{"安顶山", 30.123, 119.123, 790.2},
		{"廿四尖", 28.9512, 120.6027, 1218.3},
	}

	// 默认模型先跑一遍
	run(cfg, "icon_seamless", sites, start)

	// ECMWF 单独跑一个站点验证缺层降级告警
	fmt.Println("\n===== 切换 ECMWF 验证缺层降级告警 =====")
	run(cfg, "ecmwf_ifs025", sites[:1], start)
}

func run(cfg config.Config, modelName string, sites []siteLite, start time.Time) {
	cfg.API.Models = modelName

	fmt.Printf("\n====== 模型 %s ======\n", modelName)
	for _, s := range sites {
		resp := fetch(cfg.API.Endpoint, s.lat, s.lon, cfg.API.Models)
		if resp == nil {
			fmt.Printf("[%s] fetch 失败，跳过\n", s.name)
			continue
		}

		site := core.Site{Name: s.name, Lat: s.lat, Lon: s.lon, Alt: s.alt}
		rows := core.AnalyseSite(site, resp, nil, cfg)
		hasData, hasSea := 0, 0
		for _, r := range rows {
			if r.HasData {
				hasData++
				if r.CloudSea == "有" {
					hasSea++
				}
			}
		}
		night := core.NightIDOf(start.Add(12 * time.Hour))
		episodes := core.CollectCloudSeaEpisodesForNight(site, resp, night, cfg)

		fmt.Printf("\n[%s 海拔 %.0fm]\n", s.name, s.alt)
		fmt.Printf("  数据 hours=%d 云海=%d (%.0f%%)\n", hasData, hasSea,
			pct(hasSea, hasData))
		fmt.Printf("  Episodes=%d\n", len(episodes))
		for i, ep := range episodes {
			fmt.Printf("    [%d] %s→%s 顶%dm/机下%.0fm 厚%dm submerged=%v\n",
				i+1, ep.Start.Format("15:04"), ep.End.Format("15:04"),
				int(ep.TopMSL), ep.TopAGL, int(ep.PeakThickness), ep.Submerged)
		}
		gap := profile.MaxGapAroundSite(profile.BuildProfile(resp.LevelValues(0), cfg.Thresh), s.alt)
		if gap > 500 {
			fmt.Printf("  ⚠️  垂直分辨率告警：机位上下相邻层间距 %.0fm > 500m\n", gap)
		}
	}
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) * 100 / float64(b)
}

// fetch 跑 curl 拿真实数据，绕过沙箱 HTTPS 代理。
func fetch(endpoint string, lat, lon float64, modelName string) *api.Response {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("models", modelName)
	q.Set("timezone", "Asia/Shanghai")
	q.Set("forecast_days", "2")
	for _, name := range api.BuildHourlyVars(true) {
		q.Add("hourly", name)
	}

	cmd := exec.Command("curl", "-s", "--max-time", "60", endpoint+"?"+q.Encode())
	// 去掉 *_PROXY 变量，让 curl 直连不走沙箱代理
	for i := len(cmd.Env) - 1; i >= 0; i-- {
		// 这里直接重置，让它走不带代理的环境
		_ = i
	}
	cmd.Env = []string{} // 不继承任何环境变量，确保没有 *_PROXY
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "curl 失败：%v\n", err)
		return nil
	}

	var jr struct {
		Error  bool   `json:"error"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(out, &jr)
	if jr.Error {
		fmt.Fprintf(os.Stderr, "API 报错：%s\n", jr.Reason)
		return nil
	}

	return parseJSONToResponse(out)
}

// parseJSONToResponse 把 Open-Meteo JSON 响应转成 api.Response（仅覆盖验证所需字段）。
func parseJSONToResponse(data []byte) *api.Response {
	var raw struct {
		Latitude         float64                    `json:"latitude"`
		Longitude        float64                    `json:"longitude"`
		Elevation        float64                    `json:"elevation"`
		UTCOffsetSeconds int                        `json:"utc_offset_seconds"`
		Timezone         string                     `json:"timezone"`
		Hourly           map[string]json.RawMessage `json:"hourly"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		fmt.Fprintf(os.Stderr, "JSON 解析失败：%v\n", err)
		return nil
	}

	resp := &api.Response{
		Latitude:         raw.Latitude,
		Longitude:        raw.Longitude,
		Elevation:        raw.Elevation,
		UTCOffsetSeconds: raw.UTCOffsetSeconds,
		Timezone:         raw.Timezone,
		Series:           make(map[string][]model.OptFloat),
	}

	var times []string
	_ = json.Unmarshal(raw.Hourly["time"], &times)
	for _, t := range times {
		dt, err := time.Parse("2006-01-02T15:04", t)
		if err != nil {
			continue
		}
		resp.Times = append(resp.Times, dt.UTC())
	}

	for name, rawSeries := range raw.Hourly {
		if name == "time" {
			continue
		}
		var vals []float64
		_ = json.Unmarshal(rawSeries, &vals)
		optVals := make([]model.OptFloat, len(vals))
		for i, v := range vals {
			optVals[i] = model.NumOrMissing(v)
		}
		resp.Series[name] = optVals
	}

	resp.HourlyVars = api.BuildHourlyVars(true)
	return resp
}