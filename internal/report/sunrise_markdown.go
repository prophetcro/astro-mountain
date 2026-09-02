package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// sunriseReportFilename 推导日出报告的固定文件名：以首个结果的日出日期命名，
// 与流星雨报告的 peak/区间命名区分开，避免同一目录互相覆盖。
func sunriseReportFilename(results []SunriseSiteResult) string {
	if len(results) > 0 {
		d := results[0].SunriseTime.Format("2006-01-02")
		return fmt.Sprintf("astro_report_sunrise-%s.md", d)
	}
	return "astro_report_sunrise.md"
}

// WriteSunriseMarkdownReport 把日出模式聚合结果写出为 Markdown 报告，返回文件路径。
func WriteSunriseMarkdownReport(results []SunriseSiteResult, meta model.ReportMeta,
	cfg config.Config, outDir string) (string, error) {

	if outDir == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("创建报告目录 %s 失败：%w", outDir, err)
	}
	path := filepath.Join(outDir, sunriseReportFilename(results))
	text := BuildSunriseMarkdownReport(results, meta, cfg)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("写入报告 %s 失败：%w", path, err)
	}
	return path, nil
}

// BuildSunriseMarkdownReport 拼装日出云海模式报告的纯文本（Markdown）。
//
// 结构：元信息 → 各点位（云海时段表 / 朝霞四档 / 建议抵达时间 / 五档可信度 / 结论）
// → 综合结论汇总表 + 推荐机位。
func BuildSunriseMarkdownReport(results []SunriseSiteResult, meta model.ReportMeta,
	cfg config.Config) string {

	lines := make([]string, 0, 256)
	lines = append(lines, "# 日出云海模式评估报告", "")

	generated := meta.GeneratedAt
	if generated == "" {
		generated = "-"
	}
	w, t := cfg.Window, cfg.Thresh
	lines = append(lines,
		fmt.Sprintf("> **生成时间**：%s（本地时间）  ", generated),
		"> **说明**：云底/云顶为气压层剖面反演值，天文量为纯 Go 近似算法结果，均非观测实测值。"+
			"出发前请再跑一次以取最新预报。",
		"",
		"## 一、元信息",
		"",
	)

	sunriseDate := "-"
	if len(results) > 0 {
		sunriseDate = results[0].SunriseTime.Format("2006-01-02")
	}
	info := [][]string{
		{"运行模式", "日出云海（云海出现时间 / 距机位高度 / 消散 / 朝霞 / 建议抵达时间）"},
		{"日出当天", sunriseDate},
		{"数值模式", meta.Models},
		{"时区", fmt.Sprintf("%s (UTC+%s)", meta.Timezone, FormatG(meta.UTCOffsetHours))},
		{"夜间窗口", fmt.Sprintf("%02d:00 ~ %02d:00（含日出时分，北京时间跨零点）",
			w.NightStartHour, w.NightEndHour)},
		{"观测夜", orDash(strings.Join(meta.Nights, " / "))},
		{"云层判据", fmt.Sprintf("层云量 ≥ %s%% 或 RH ≥ %s%%(低层)/%s%%(高层)",
			FormatFixed(t.CloudCoverThreshold, 0), FormatFixed(t.RHThresholdLow, 0),
			FormatFixed(t.RHThresholdHigh, 0))},
		{"生成时间", generated},
	}
	lines = append(lines, MDTable([]string{"项目", "内容"}, info)...)

	lines = append(lines, "", "### 1.1 点位列表", "")
	siteRows := make([][]string, 0, len(meta.Sites))
	for _, s := range meta.Sites {
		siteRows = append(siteRows, []string{s.Name, FormatFixed(s.Lat, 4), FormatFixed(s.Lon, 4), FormatG(s.Alt)})
	}
	lines = append(lines, MDTable([]string{"点位", "纬度", "经度", "海拔(m)"}, siteRows)...)
	lines = append(lines, "")

	// 二、各点位日出云海评估
	lines = append(lines, "## 二、各点位日出云海评估", "")
	for _, r := range results {
		lines = append(lines, fmt.Sprintf("### %s", r.Site), "")
		lines = append(lines, fmt.Sprintf("- 日出时刻：**%s**   建议抵达机位：**%s**",
			r.SunriseTime.Format("2006-01-02 15:04"),
			r.ArriveBy.Format("2006-01-02 15:04")), "")

		if len(r.Episodes) == 0 {
			lines = append(lines, "**云海时段**：该夜未检出连续云面（机位下方无云海）。", "")
		} else {
			lines = append(lines,
				fmt.Sprintf("**云海时段**：%d 段，共 %d 小时", len(r.Episodes), r.CloudSeaHours), "")
			epRows := make([][]string, 0, len(r.Episodes))
			for i, ep := range r.Episodes {
				epRows = append(epRows, []string{
					fmt.Sprintf("%d", i+1),
					ep.Start.Format("15:04"),
					ep.End.Format("15:04"),
					fmt.Sprintf("%d", ep.HoursCount),
					episodeHeightLabel(ep),
					fmt.Sprintf("%.0f", ep.PeakThickness),
					boolCN(ep.Submerged, "是", "否"),
				})
			}
			lines = append(lines,
				MDTable([]string{"#", "出现", "消散", "时长h", "云顶距机位", "云厚m", "淹没机位"}, epRows)...)
			lines = append(lines, "")
		}

		lines = append(lines,
			fmt.Sprintf("**朝霞强度**：%s — %s", r.DawnGlow, r.DawnGlowNote), "",
			fmt.Sprintf("**诚实五档可信度**：%s — %s", r.Confidence, r.ConfidenceNote), "",
			fmt.Sprintf("**一句话结论**：%s", r.Rating), "",
		)
	}

	// 三、综合结论
	lines = append(lines, "## 三、综合结论", "")
	if len(results) == 0 {
		lines = append(lines, "本次运行未解析出任何站点结果。", "")
	} else {
		sumRows := make([][]string, 0, len(results))
		for _, r := range results {
			sumRows = append(sumRows, []string{
				r.Site,
				fmt.Sprintf("%d", r.CloudSeaHours),
				r.DawnGlow,
				r.Confidence,
				r.ArriveBy.Format("15:04"),
				r.Rating,
			})
		}
		lines = append(lines,
			MDTable([]string{"点位", "云海时长h", "朝霞", "可信度", "建议抵达", "结论"}, sumRows)...)
		lines = append(lines, "")
		if best := bestSunriseSite(results); best != "" {
			lines = append(lines, fmt.Sprintf("**综合推荐**：%s（云海时长与可信度综合最优）", best))
		}
	}

	lines = append(lines, "---", "",
		fmt.Sprintf("*由 `astro-mountain` 自动生成 · 模式 `%s`。%s*", meta.Models, meta.Disclaimer), "")

	return strings.Join(lines, "\n")
}

// PrintSunriseReport 在终端紧凑打印日出模式结果。
func PrintSunriseReport(w io.Writer, results []SunriseSiteResult, meta model.ReportMeta, cfg config.Config) {
	_ = cfg
	width := 56
	line := Repeat("=", width)
	dash := Repeat("-", width)

	fmt.Fprintln(w, line)
	fmt.Fprintln(w, "日出云海模式评估报告  （气压层剖面反演，非 LCL 估算）")
	fmt.Fprintln(w, dash)
	if len(results) > 0 {
		fmt.Fprintf(w, "日出当天   : %s\n", results[0].SunriseTime.Format("2006-01-02"))
	}
	fmt.Fprintf(w, "数值模式   : %s\n", meta.Models)
	names := make([]string, 0, len(meta.Sites))
	for _, s := range meta.Sites {
		names = append(names, fmt.Sprintf("%s(%sm)", s.Name, FormatG(s.Alt)))
	}
	fmt.Fprintf(w, "点位(%d) : %s\n", len(meta.Sites), strings.Join(names, "  "))
	fmt.Fprintln(w, dash)

	for _, r := range results {
		fmt.Fprintf(w, "■ %s\n", r.Site)
		fmt.Fprintf(w, "  日出 %s   建议抵达 %s\n",
			r.SunriseTime.Format("15:04"), r.ArriveBy.Format("01-02 15:04"))
		if len(r.Episodes) == 0 {
			fmt.Fprintln(w, "  云海：未检出")
		} else {
			fmt.Fprintf(w, "  云海：%d 段 / 共 %d h\n", len(r.Episodes), r.CloudSeaHours)
			for i, ep := range r.Episodes {
				fmt.Fprintf(w, "    [%d] %s→%s  %s  厚%.0fm\n", i+1,
					ep.Start.Format("15:04"), ep.End.Format("15:04"),
					episodeHeightLabel(ep), ep.PeakThickness)
			}
		}
		fmt.Fprintf(w, "  朝霞：%s\n", r.DawnGlow)
		fmt.Fprintf(w, "  可信度：%s\n", r.Confidence)
		fmt.Fprintf(w, "  结论：%s\n", r.Rating)
	}

	if best := bestSunriseSite(results); best != "" {
		fmt.Fprintf(w, "➜ 综合推荐：%s\n", best)
	}
	fmt.Fprintln(w, line)
}

// episodeHeightLabel 把云顶距机位高差转成可读文案：正=在脚下、负=淹没机位。
func episodeHeightLabel(ep CloudSeaEpisode) string {
	if ep.Submerged {
		return fmt.Sprintf("淹没机位（云顶高于机位 %.0fm）", -ep.TopAGL)
	}
	return fmt.Sprintf("机下 %.0fm", ep.TopAGL)
}

// confidenceRank 给诚实五档可信度赋序，供综合推荐排序。
func confidenceRank(c string) int {
	switch c {
	case "极高":
		return 5
	case "高":
		return 4
	case "中":
		return 3
	case "低":
		return 2
	case "极低":
		return 1
	default:
		return 0
	}
}

// bestSunriseSite 按「云海时长 × 10 + 可信度序」给出综合最优机位名。
func bestSunriseSite(results []SunriseSiteResult) string {
	best := ""
	bestScore := -1
	for _, r := range results {
		score := r.CloudSeaHours*10 + confidenceRank(r.Confidence)
		if score > bestScore {
			bestScore = score
			best = r.Site
		}
	}
	return best
}
