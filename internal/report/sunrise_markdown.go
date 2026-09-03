package report

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// sunriseResultDate 取某条日出结果所属的「日出当天」key（用于多日分组）。
// 优先用显式填好的 SunriseDate 字段；未填时回落 SunriseTime 的日期（兼容旧调用方）。
func sunriseResultDate(r SunriseSiteResult) string {
	if r.SunriseDate != "" {
		return r.SunriseDate
	}
	return r.SunriseTime.Format("2006-01-02")
}

// sunriseResultsByDate 把结果按「日出当天」分组，并返回升序的日期列表。
// 多日模式据此把报告分节；单日模式退化为只有一个分组的列表，调用方据此判断要不要加日期小标题。
func sunriseResultsByDate(results []SunriseSiteResult) ([]string, map[string][]SunriseSiteResult) {
	m := make(map[string][]SunriseSiteResult, 8)
	for _, r := range results {
		k := sunriseResultDate(r)
		m[k] = append(m[k], r)
	}
	order := make([]string, 0, len(m))
	for k := range m {
		order = append(order, k)
	}
	sort.Strings(order)
	return order, m
}

// sunriseNightDate 由「日出当天」反推观测夜（前一日，YYYY-MM-DD）。
// 日出云海的实际拍摄窗口在日出当天的前一夜，报告按观测夜标注才贴合「前一天」的心智，
// 而不是孤立地强调「日出当天」。
func sunriseNightDate(sunriseDay string) string {
	t, err := time.Parse("2006-01-02", sunriseDay)
	if err != nil {
		return sunriseDay
	}
	return t.AddDate(0, 0, -1).Format("2006-01-02")
}

// sunriseNightLabel 生成某日出当天对应的「观测夜 → 日出」标签，把真正拍摄的
// 「前一天」（观测夜）显式标出来，不再重复强调「日出当天」。
func sunriseNightLabel(sunriseDay string) string {
	return fmt.Sprintf("观测夜 %s（日出 %s）", sunriseNightDate(sunriseDay), sunriseDay)
}

// sunriseResultsBySite 把结果按站点分组，组内按「日出当天」升序，并返回稳定的站点顺序
// （按站点名排序，保证渲染一致）。多日模式据此把「各点位评估」小节从「按日期分节」改为
// 「按站点分节」——否则同一站点会散落在多个日期 H3 标题下，在折叠重复锚点的 Markdown 预览器
// 里用户只看到第一天，误以为「选了三天却只有一天」。
func sunriseResultsBySite(results []SunriseSiteResult) ([]string, map[string][]SunriseSiteResult) {
	m := make(map[string][]SunriseSiteResult, 8)
	for _, r := range results {
		m[r.Site] = append(m[r.Site], r)
	}
	order := make([]string, 0, len(m))
	for k := range m {
		order = append(order, k)
	}
	sort.Strings(order)
	for _, k := range order {
		s := m[k]
		sort.SliceStable(s, func(i, j int) bool {
			return sunriseResultDate(s[i]) < sunriseResultDate(s[j])
		})
		m[k] = s
	}
	return order, m
}

// sunriseReportFilename 推导日出报告的固定文件名：以日出当天命名，
// 与流星雨报告的 peak/区间命名区分开，避免同一目录互相覆盖。
// 多日模式用「首_末」区间命名；单日模式保持原样（astro_report_sunrise-YYYY-MM-DD.md）。
func sunriseReportFilename(results []SunriseSiteResult) string {
	if len(results) == 0 {
		return "astro_report_sunrise.md"
	}
	order, _ := sunriseResultsByDate(results)
	if len(order) == 1 {
		return fmt.Sprintf("astro_report_sunrise-%s.md", order[0])
	}
	return fmt.Sprintf("astro_report_sunrise-%s_%s.md", order[0], order[len(order)-1])
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
// 结构：元信息 → 各点位（云海时段表 / 云海形态 / 朝霞四档 / 建议抵达时间 / 云海可信度 / 结论）
// → 综合结论汇总表 + 推荐机位。
func BuildSunriseMarkdownReport(results []SunriseSiteResult, meta model.ReportMeta,
	cfg config.Config) string {

	lines := make([]string, 0, 256)
	lines = append(lines, "# 日出云海模式评估报告", "")

	order, byDate := sunriseResultsByDate(results)
	siteOrder, bySite := sunriseResultsBySite(results)
	multi := len(order) > 1

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
		if multi {
			sunriseDate = fmt.Sprintf("%d 天（%s ~ %s）", len(order), order[0], order[len(order)-1])
		} else {
			sunriseDate = order[0]
		}
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

	// 二、各点位日出云海评估。
	// 多日模式按「站点」分节（每个站点一个 H3，其下按日期升序列出各天），
	// 避免同一站点散落在多个「日出当天」H3 标题下、在折叠重复锚点的预览器里只显第一天。
	lines = append(lines, "## 二、各点位日出云海评估", "")
	if multi {
		for _, site := range siteOrder {
			lines = append(lines, fmt.Sprintf("### %s", site), "")
			for _, r := range bySite[site] {
				lines = append(lines, fmt.Sprintf("#### %s", sunriseNightLabel(sunriseResultDate(r))), "")
				lines = append(lines, sunriseSiteDayBody(r)...)
			}
		}
	} else {
		for _, r := range results {
			lines = append(lines, sunriseSiteDetail(r)...)
		}
	}

	// 三、综合结论（多日模式按日出当天分节，单日模式沿用原无小标题结构）
	lines = append(lines, "## 三、综合结论", "")
	if len(results) == 0 {
		lines = append(lines, "本次运行未解析出任何站点结果。", "")
	} else if multi {
		for _, d := range order {
			lines = append(lines, fmt.Sprintf("### %s", sunriseNightLabel(d)), "")
			sumRows := make([][]string, 0, len(byDate[d]))
			for _, r := range byDate[d] {
				sumRows = append(sumRows, []string{
					r.Site,
					fmt.Sprintf("%d", r.CloudSeaHours),
					orDash(r.CloudSeaForm),
					r.DawnGlow,
					r.Confidence,
					r.ArriveBy.Format("15:04"),
					r.Rating,
				})
			}
			lines = append(lines,
				MDTable([]string{"点位", "云海时长h", "云海形态", "朝霞", "云海可信度", "建议抵达", "结论"}, sumRows)...)
			lines = append(lines, "")
			if best := bestSunriseSite(byDate[d]); best != "" {
				lines = append(lines, fmt.Sprintf("**综合推荐（该日出）**：%s（云海时长与可信度综合最优）", best))
			}
			lines = append(lines, "")
		}
	} else {
		sumRows := make([][]string, 0, len(results))
		for _, r := range results {
			sumRows = append(sumRows, []string{
				r.Site,
				fmt.Sprintf("%d", r.CloudSeaHours),
				orDash(r.CloudSeaForm),
				r.DawnGlow,
				r.Confidence,
				r.ArriveBy.Format("15:04"),
				r.Rating,
			})
		}
		lines = append(lines,
			MDTable([]string{"点位", "云海时长h", "云海形态", "朝霞", "云海可信度", "建议抵达", "结论"}, sumRows)...)
		lines = append(lines, "")
		if best := bestSunriseSite(results); best != "" {
			lines = append(lines, fmt.Sprintf("**综合推荐**：%s（云海时长与可信度综合最优）", best))
		}
	}

	lines = append(lines, "---", "",
		fmt.Sprintf("*由 `astro-mountain` 自动生成 · 模式 `%s`。%s*", meta.Models, meta.Disclaimer), "")

	return strings.Join(lines, "\n")
}

// sunriseSiteDetail 拼装单站点日出云海评估的 Markdown 片段（站点标题 + 单日正文）。
// 单日模式直接用本函数；多日模式改用 sunriseSiteDayBody（不含站点标题），由调用方先写站点 H3。
func sunriseSiteDetail(r SunriseSiteResult) []string {
	out := make([]string, 0, 24)
	out = append(out, fmt.Sprintf("### %s", r.Site), "")
	out = append(out, sunriseSiteDayBody(r)...)
	return out
}

// sunriseSiteDayBody 拼装单站点单日（某日出当天）的评估正文，不含站点 H3 标题，
// 供单日整段与多日「按站点分节、站点下逐日」两种结构共用，保证字段渲染完全一致
// （尤其「云海可信度」「云海形态」「朝霞强度」标签）。
func sunriseSiteDayBody(r SunriseSiteResult) []string {
	out := make([]string, 0, 22)
	out = append(out, fmt.Sprintf("- 日出时刻：**%s**   建议抵达机位：**%s**",
		r.SunriseTime.Format("2006-01-02 15:04"),
		r.ArriveBy.Format("2006-01-02 15:04")), "")

	if len(r.Episodes) == 0 {
		out = append(out, "**云海时段**：该夜未检出连续云面（机位下方无云海）。", "")
	} else {
		out = append(out,
			fmt.Sprintf("**云海时段**：%d 段，共 %d 小时", len(r.Episodes), r.CloudSeaHours), "")
		epRows := make([][]string, 0, len(r.Episodes))
		for i, ep := range r.Episodes {
			epRows = append(epRows, []string{
				fmt.Sprintf("%d", i+1),
				ep.Start.Format("15:04"),
				ep.End.Format("15:04"),
				episodeHoursLabel(ep),
				episodeHeightLabel(ep),
				fmt.Sprintf("%.0f", ep.PeakThickness),
				boolCN(ep.Submerged, "是", "否"),
			})
		}
		out = append(out,
			MDTable([]string{"#", "出现", "消散", "时长h", "云顶距机位", "云厚m", "淹没机位"}, epRows)...)
		out = append(out, "")
	}

	if r.CloudSeaForm != "" {
		out = append(out, fmt.Sprintf("**云海形态**：%s", r.CloudSeaForm), "")
	}
	out = append(out,
		fmt.Sprintf("**朝霞强度**：%s — %s", r.DawnGlow, r.DawnGlowNote), "",
		fmt.Sprintf("**云海可信度**：%s — %s", r.Confidence, r.ConfidenceNote), "",
		fmt.Sprintf("**一句话结论**：%s", r.Rating), "",
	)
	return out
}

// PrintSunriseReport 在终端紧凑打印日出模式结果。
// 多日模式按「站点」分节（每个站点下逐日列出），与 Markdown 报告一致；
// 末尾再逐日给出「综合推荐」，保留按天的口径。
func PrintSunriseReport(w io.Writer, results []SunriseSiteResult, meta model.ReportMeta, cfg config.Config) {
	_ = cfg
	width := 56
	line := Repeat("=", width)
	dash := Repeat("-", width)

	fmt.Fprintln(w, line)
	fmt.Fprintln(w, "日出云海模式评估报告  （气压层剖面反演，非 LCL 估算）")
	fmt.Fprintln(w, dash)
	order, byDate := sunriseResultsByDate(results)
	if len(results) > 0 {
		if len(order) > 1 {
			fmt.Fprintf(w, "日出当天   : %d 天（%s ~ %s）\n", len(order), order[0], order[len(order)-1])
		} else {
			fmt.Fprintf(w, "日出当天   : %s\n", order[0])
		}
	}
	fmt.Fprintf(w, "数值模式   : %s\n", meta.Models)
	names := make([]string, 0, len(meta.Sites))
	for _, s := range meta.Sites {
		names = append(names, fmt.Sprintf("%s(%sm)", s.Name, FormatG(s.Alt)))
	}
	fmt.Fprintf(w, "点位(%d) : %s\n", len(meta.Sites), strings.Join(names, "  "))
	fmt.Fprintln(w, dash)

	if len(order) > 1 {
		siteOrder, bySite := sunriseResultsBySite(results)
		for _, site := range siteOrder {
			fmt.Fprintf(w, "■ %s\n", site)
			for _, r := range bySite[site] {
				fmt.Fprintf(w, "  ── 观测夜 %s → 日出 %s ──\n",
					sunriseNightDate(sunriseResultDate(r)), sunriseResultDate(r))
				printSunriseSiteDay(w, r)
			}
		}
		for _, d := range order {
			if best := bestSunriseSite(byDate[d]); best != "" {
				fmt.Fprintf(w, "➜ 综合推荐(%s)：%s\n", d, best)
			}
		}
	} else {
		printSunriseSiteBlock(w, results)
		if best := bestSunriseSite(results); best != "" {
			fmt.Fprintf(w, "➜ 综合推荐：%s\n", best)
		}
	}
	fmt.Fprintln(w, line)
}

// printSunriseSiteDay 打印单站点单日（某日出当天）的终端明细，不含 ■ 站点标题，
// 供多日「按站点分节」结构在站点标题下逐日复用。
func printSunriseSiteDay(w io.Writer, r SunriseSiteResult) {
	fmt.Fprintf(w, "  日出 %s   建议抵达 %s\n",
		r.SunriseTime.Format("15:04"), r.ArriveBy.Format("01-02 15:04"))
	if len(r.Episodes) == 0 {
		fmt.Fprintln(w, "  云海：未检出")
	} else {
		fmt.Fprintf(w, "  云海：%d 段 / 共 %d h\n", len(r.Episodes), r.CloudSeaHours)
		for i, ep := range r.Episodes {
			fmt.Fprintf(w, "    [%d] %s→%s  %dh  %s  厚%.0fm\n", i+1,
				ep.Start.Format("15:04"), ep.End.Format("15:04"), ep.HoursCount,
				episodeHeightLabel(ep), ep.PeakThickness)
			if ep.MissingHours > 0 {
				fmt.Fprintf(w, "        注：该时段中间有 %d 个时次廓线缺测，未计入时长\n",
					ep.MissingHours)
			}
		}
	}
	if r.CloudSeaForm != "" {
		fmt.Fprintf(w, "  云海形态：%s\n", r.CloudSeaForm)
	}
	fmt.Fprintf(w, "  朝霞：%s\n", r.DawnGlow)
	fmt.Fprintf(w, "  云海可信度：%s\n", r.Confidence)
	fmt.Fprintf(w, "  结论：%s\n", r.Rating)
}

// printSunriseSiteBlock 打印一组站点（同一日出当天）的终端明细块。
func printSunriseSiteBlock(w io.Writer, results []SunriseSiteResult) {
	for _, r := range results {
		fmt.Fprintf(w, "■ %s\n", r.Site)
		fmt.Fprintf(w, "  日出 %s   建议抵达 %s\n",
			r.SunriseTime.Format("15:04"), r.ArriveBy.Format("01-02 15:04"))
		if len(r.Episodes) == 0 {
			fmt.Fprintln(w, "  云海：未检出")
		} else {
			fmt.Fprintf(w, "  云海：%d 段 / 共 %d h\n", len(r.Episodes), r.CloudSeaHours)
			for i, ep := range r.Episodes {
				fmt.Fprintf(w, "    [%d] %s→%s  %dh  %s  厚%.0fm\n", i+1,
					ep.Start.Format("15:04"), ep.End.Format("15:04"), ep.HoursCount,
					episodeHeightLabel(ep), ep.PeakThickness)
				if ep.MissingHours > 0 {
					fmt.Fprintf(w, "        注：该时段中间有 %d 个时次廓线缺测，未计入时长\n",
						ep.MissingHours)
				}
			}
		}
		if r.CloudSeaForm != "" {
			fmt.Fprintf(w, "  云海形态：%s\n", r.CloudSeaForm)
		}
		fmt.Fprintf(w, "  朝霞：%s\n", r.DawnGlow)
		fmt.Fprintf(w, "  云海可信度：%s\n", r.Confidence)
		fmt.Fprintf(w, "  结论：%s\n", r.Rating)
	}
}

// episodeHeightLabel 把云顶距机位高差转成可读文案：正=在脚下、负=淹没机位。
//
// 淹没型即高山云海典型形态：云从山脚一路堆过机位、脚下没有独立层，
// 机位处在云层顶部附近。2026-09 之前 Submerged 恒为 false（判定条件与
// HighestBeneath 互斥），这个分支永远进不来；统一到 ClassifySeaGeometry 后才复活。
func episodeHeightLabel(ep CloudSeaEpisode) string {
	if ep.Submerged {
		return fmt.Sprintf("淹没机位（云顶高于机位 %.0fm）", -ep.TopAGL)
	}
	return fmt.Sprintf("机下 %.0fm", ep.TopAGL)
}

// episodeHoursLabel 渲染时长，缺测时次单独标注。
//
// 缺测不计入时长（缺测不等于有云海），但也不切断时段（缺测同样不等于云海散了），
// 所以这里如实写出「含 N 时次缺测」，让用户知道这段时长里有几小时是模式没给的。
func episodeHoursLabel(ep CloudSeaEpisode) string {
	if ep.MissingHours > 0 {
		return fmt.Sprintf("%d（含 %d 时次缺测）", ep.HoursCount, ep.MissingHours)
	}
	return fmt.Sprintf("%d", ep.HoursCount)
}

// confidenceRank 给云海可信度五档赋序，供综合推荐排序。
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
