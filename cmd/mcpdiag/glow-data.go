package main

// glow-data —— 单点日出/日落时刻逐模型云量对照，用来诊断「bot 报大烧 vs 火烧云报不烧」分歧。
//
// 起因：用户用 sunsetbot 查宣城-绩溪县 9-06 日出，鲜艳度 0.001（不烧）。
// 但 astro-mountain 默认 icon_seamless 报中云 40% → 大烧。两种模型/两种工具结论相反。
// 本工具把同一坐标/同一时刻对多个 NWP 模型的逐时云量并排打出来，肉眼即可定位是哪个模型离群。
//
// 用法:
//   mcpdiag glow-data --lat 30.07 --lon 118.58 --date 2026-09-06 --hour 5
//   mcpdiag glow-data --lat 30.07 --lon 118.58 --date 2026-09-06 --hour 5 --models icon_seamless,gfs_seamless
//   mcpdiag glow-data -h

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultModels = "icon_seamless,gfs_seamless,ecmwf_ifs025,best_match"
	defaultTZ     = "Asia/Shanghai"
)

type omHourly struct {
	Time             []string `json:"time"`
	CloudCover       []int    `json:"cloud_cover"`
	CloudCoverLow    []int    `json:"cloud_cover_low"`
	CloudCoverMid    []int    `json:"cloud_cover_mid"`
	CloudCoverHigh   []int    `json:"cloud_cover_high"`
	AerosolOpticalD  []any    `json:"aerosol_optical_depth"`
}

type omResponse struct {
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	UTCOff    int       `json:"utc_offset_seconds"`
	Hourly    omHourly  `json:"hourly"`
	Error     bool      `json:"error"`
	Reason    string    `json:"reason"`
}

func runGlowData(args []string) error {
	fs := flag.NewFlagSet("glow-data", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // 我们自己处理 -h
	lat := fs.Float64("lat", 0, "纬度（必填）")
	lon := fs.Float64("lon", 0, "经度（必填）")
	date := fs.String("date", "", "日期 YYYY-MM-DD（必填）")
	hour := fs.Int("hour", 5, "目标小时（当地墙钟，整点 0-23）")
	models := fs.String("models", defaultModels, "逗号分隔的模型列表")
	tz := fs.String("tz", defaultTZ, "Open-Meteo timezone 参数")
	asJSON := fs.Bool("json", false, "输出 JSON 而非文本表格")
	help := fs.Bool("h", false, "显示帮助")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *help {
		printGlowDataUsage(os.Stdout)
		return nil
	}
	if *lat == 0 && *lon == 0 {
		printGlowDataUsage(os.Stderr)
		return fmt.Errorf("必须提供 --lat 与 --lon")
	}
	if *date == "" {
		return fmt.Errorf("必须提供 --date YYYY-MM-DD")
	}

	day, err := time.Parse("2006-01-02", *date)
	if err != nil {
		return fmt.Errorf("日期格式错：%v", err)
	}
	// 抓 ±12h（覆盖前一天傍晚+当天日出+上午）
	startDate := day.Add(-12 * time.Hour).Format("2006-01-02")
	endDate := day.Add(12 * time.Hour).Format("2006-01-02")

	client := &http.Client{Timeout: 25 * time.Second}

	type row struct {
		Model    string         `json:"model"`
		Result   map[string][4]int `json:"rows"` // hour -> [low, mid, high, total]
		Err      string         `json:"error,omitempty"`
	}
	results := make([]row, 0, 8)

	for _, m := range strings.Split(*models, ",") {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		r := row{Model: m, Result: map[string][4]int{}}
		url := buildURL(*lat, *lon, startDate, endDate, m, *tz)
		resp, err := client.Get(url)
		if err != nil {
			r.Err = fmt.Sprintf("请求失败: %v", err)
			results = append(results, r)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var data omResponse
		if jerr := json.Unmarshal(body, &data); jerr != nil {
			r.Err = fmt.Sprintf("解析失败: %v", jerr)
			results = append(results, r)
			continue
		}
		if data.Error {
			r.Err = fmt.Sprintf("API 错误: %s", data.Reason)
			results = append(results, r)
			continue
		}
		for i, t := range data.Hourly.Time {
			// 只取 day 当天 ±2h 内的时次，避免输出过长
			ts, _ := time.Parse("2006-01-02T15:04", t)
			if ts.IsZero() {
				ts, _ = time.Parse("2006-01-02T15:04:05", t)
			}
			if ts.IsZero() {
				continue
			}
			diff := ts.Sub(day)
			if diff < -3*time.Hour || diff > 6*time.Hour {
				continue
			}
			key := t[:13] // 截到小时
			r.Result[key] = [4]int{
				safeInt(data.Hourly.CloudCoverLow, i),
				safeInt(data.Hourly.CloudCoverMid, i),
				safeInt(data.Hourly.CloudCoverHigh, i),
				safeInt(data.Hourly.CloudCover, i),
			}
		}
		results = append(results, r)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Lat     float64 `json:"lat"`
			Lon     float64 `json:"lon"`
			Date    string  `json:"date"`
			Hour    int     `json:"hour"`
			Results []row   `json:"results"`
		}{*lat, *lon, *date, *hour, results})
	}

	fmt.Printf("📍 (%.4f, %.4f)  日期 %s  目标小时 %02d:00 %s\n", *lat, *lon, *date, *hour, *tz)
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════════════")
	for _, r := range results {
		if r.Err != "" {
			fmt.Printf("【%-18s】 %s\n", r.Model, r.Err)
			continue
		}
		fmt.Printf("【%s】\n", r.Model)
		fmt.Println("  时次                  低%   中%   高%   总%")
		// 按时间排序打印
		keys := sortedKeys(r.Result)
		prefix := fmt.Sprintf("%sT%02d", *date, *hour)
		for _, k := range keys {
			v := r.Result[k]
			marker := ""
			if strings.HasPrefix(k, prefix) {
				marker = "  ← 目标小时"
			}
			fmt.Printf("  %-21s %3d  %3d  %3d  %3d%s\n", k, v[0], v[1], v[2], v[3], marker)
		}
		fmt.Println("───────────────────────────────────────────────────────────────────────────────────────")
	}
	return nil
}

func buildURL(lat, lon float64, startDate, endDate, model, tz string) string {
	return fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%g&longitude=%g"+
			"&hourly=cloud_cover,cloud_cover_low,cloud_cover_mid,cloud_cover_high"+
			"&models=%s&start_date=%s&end_date=%s&timezone=%s",
		lat, lon, model, startDate, endDate, urlEncode(tz))
}

func urlEncode(s string) string {
	// 极简：只编码空格、冒号、斜杠，Open-Meteo 时区形如 Asia/Shanghai
	r := strings.NewReplacer(":", "%3A", "/", "%2F")
	return r.Replace(s)
}

func safeInt(s []int, i int) int {
	if i < 0 || i >= len(s) {
		return -1
	}
	return s[i]
}

func sortedKeys(m map[string][4]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// 已是 ISO 小时前缀，字典序就是时间序
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

func printGlowDataUsage(w *os.File) {
	fmt.Fprintln(w, "mcpdiag glow-data — 单点日出/日落时刻逐模型云量对照")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "用法:")
	fmt.Fprintln(w, "  mcpdiag glow-data --lat <float> --lon <float> --date YYYY-MM-DD [--hour N]")
	fmt.Fprintln(w, "                   [--models m1,m2,...] [--tz Area/City] [--json] [--all]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "示例:")
	fmt.Fprintln(w, "  mcpdiag glow-data --lat 30.07 --lon 118.58 --date 2026-09-06 --hour 5")
	fmt.Fprintln(w, "  mcpdiag glow-data --lat 30.07 --lon 118.58 --date 2026-09-06 --hour 5 --json")
	fmt.Fprintln(w, "  mcpdiag glow-data --lat 30.07 --lon 118.58 --date 2026-09-06 --hour 5 \\")
	fmt.Fprintln(w, "                   --models icon_seamless,gfs_seamless")
}
