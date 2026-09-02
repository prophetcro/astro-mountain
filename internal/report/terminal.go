package report

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/astro"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

type TableCol struct {
	Title string
	Width int
	Align string
}

var TableCols = []TableCol{
	{"点位", 10, AlignLeft},
	{"时间(北京)", 12, AlignLeft},
	{"云底AGL", 9, AlignRight},
	{"云顶AGL", 9, AlignRight},
	{"云厚m", 7, AlignRight},
	{"低云%", 6, AlignRight},
	{"能见度m", 8, AlignRight},
	{"评级", 8, AlignLeft},
	{"判断说明", 0, AlignLeft},
}

var TableFixedWidth = func() int {
	sum := 0
	for _, c := range TableCols {
		sum += c.Width
	}
	return sum + len(TableCols) - 1
}()

func fmtRowCells(row model.HourRow) []string {
	return []string{
		row.Site,
		row.TimeShort,
		FmtInt(row.CloudBaseAGL),
		FmtInt(row.CloudTopAGL),
		FmtInt(row.CloudThickness),
		FmtInt(row.CloudLow),
		FmtInt(row.Visibility),
		row.Rating,
		row.Note,
	}
}

func renderLine(cells []string) string {
	out := make([]string, 0, len(TableCols))
	for i, col := range TableCols {
		var v string
		if i < len(cells) {
			v = cells[i]
		}
		if v == "" {
			v = MissingCell
		}
		if col.Width == 0 {
			out = append(out, v)
		} else {
			out = append(out, Pad(v, col.Width, col.Align))
		}
	}
	return strings.TrimRight(strings.Join(out, " "), " ")
}

// sourceProductLabel 按数据源返回终端表头里展示的产品名，避免 C 轨（Meteoblue）
// 被误写成 Open-Meteo 免费 API。
func sourceProductLabel(source string) string {
	switch source {
	case MetaSourceMeteoblue:
		return "Meteoblue 融合预报（分层云量，不反演云海几何）"
	case MetaSourceTomorrow:
		return TomorrowTrackLabel
	default:
		return "Open-Meteo 免费 API"
	}
}

func PrintHeader(w io.Writer, meta model.ReportMeta, cfg config.Config) {
	t, win := cfg.Thresh, cfg.Window
	line := Repeat("=", TableFixedWidth)
	dash := Repeat("-", TableFixedWidth)

	fmt.Fprintln(w, line)
	fmt.Fprintln(w, "山地星空 / 流星雨 低云海拔评估  v2   （气压层剖面反演，非 LCL 估算）")
	fmt.Fprintln(w, dash)
	fmt.Fprintf(w, "数据源     : %s\n", sourceProductLabel(meta.Source))
	if meta.Source == MetaSourceOpenMeteo {
		fmt.Fprintf(w, "数值模式   : %s\n", meta.Models)
	}
	fmt.Fprintf(w, "查询范围   : %s ~ %s   时区 Asia/Shanghai (UTC+%s)\n",
		meta.Start, meta.End, FormatG(meta.UTCOffsetHours))
	fmt.Fprintf(w, "观测夜     : %s\n", meta.NightsDesc)
	fmt.Fprintf(w, "夜间窗口   : %02d:00 ~ %02d:00（北京时间，跨零点）\n",
		win.NightStartHour, win.NightEndHour)

	names := make([]string, 0, len(meta.Sites))
	for _, s := range meta.Sites {
		names = append(names, fmt.Sprintf("%s(%sm)", s.Name, FormatG(s.Alt)))
	}
	fmt.Fprintf(w, "点位(%d) : %s\n", len(meta.Sites), strings.Join(names, "  "))

	fmt.Fprintf(w, "判据       : 层云量>=%s%% 或 RH>=%s%%(低层)/%s%%(高层)；能见度<%sm 判雾\n",
		FormatFixed(t.CloudCoverThreshold, 0), FormatFixed(t.RHThresholdLow, 0),
		FormatFixed(t.RHThresholdHigh, 0), FormatFixed(t.FogVisibilityM, 0))
	fmt.Fprintln(w, "列义       : 云底/云顶AGL 为相对机位高度，负值=在脚下、跨零=机位在云中")
	fmt.Fprintln(w, "备注       : LCL 为经验估算量（非云底观测值），仅作辐射雾辅助指标，不参与评级")
	if !meta.VisibilityAvailable {
		fmt.Fprintf(w, "注意       : 该模式在本区域不提供 visibility，雾判据退化为近地 RH 代理（>=%s%% 判雾）\n",
			FormatFixed(t.FogProxyRHHigh, 0))
	}
	fmt.Fprintln(w, line)
}

func PrintNightBlock(w io.Writer, night string, rows []model.HourRow,
	compare []model.ModelCompareRow, sites []model.Site, cfg config.Config, utcOffsetSec int) {

	fmt.Fprintln(w)
	fmt.Fprintln(w, Repeat("#", TableFixedWidth))
	fmt.Fprintf(w, "■ 观测夜 %s（%s 22:00 → 次日 06:00）   %s\n",
		night, night, nightAstroLine(night, sites, cfg, utcOffsetSec))
	fmt.Fprintln(w, Repeat("#", TableFixedWidth))

	valid := make([]model.HourRow, 0, len(rows))
	for _, r := range rows {
		if r.HasData {
			valid = append(valid, r)
		}
	}
	missing := len(rows) - len(valid)
	if len(valid) == 0 {
		fmt.Fprintln(w, "  ⛔ 该夜全部时次均无有效预报数据（数值模式未返回气压层云量等必要字段，或确实超出预报时效）。")
		fmt.Fprintln(w, "     → 请改用 icon_seamless 或 gfs_seamless，或在极大日前 5~7 天再跑。")
		return
	}
	if missing > 0 {
		fmt.Fprintf(w, "  ⚠️ 该夜 %d/%d 个时次超出预报时效，下表仅列出有数据的时次。\n",
			missing, len(rows))
	}

	titles := make([]string, 0, len(TableCols))
	for _, c := range TableCols {
		titles = append(titles, c.Title)
	}
	fmt.Fprintln(w, renderLine(titles))
	fmt.Fprintln(w, Repeat("-", TableFixedWidth))

	sorted := append([]model.HourRow(nil), valid...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].TimeISO != sorted[j].TimeISO {
			return sorted[i].TimeISO < sorted[j].TimeISO
		}
		return sorted[i].Site < sorted[j].Site
	})
	for _, r := range sorted {
		fmt.Fprintln(w, renderLine(fmtRowCells(r)))
	}

	fmt.Fprintln(w, Repeat("-", TableFixedWidth))
	nightCompare := make([]model.ModelCompareRow, 0, len(compare))
	for _, c := range compare {
		if c.Night == night {
			nightCompare = append(nightCompare, c)
		}
	}
	fmt.Fprintf(w, "【%s 夜 · 各点位结论】\n", night)
	for _, site := range sites {
		if c := SummariseSiteNight(site.Name, rows, nightCompare, cfg); c != "" {
			fmt.Fprintln(w, "  "+c)
		}
	}
}

func PrintOverview(w io.Writer, rows []model.HourRow, compare []model.ModelCompareRow,
	sites []model.Site, nights []string, models string, cfg config.Config) {

	win := cfg.Window
	fmt.Fprintln(w)
	fmt.Fprintln(w, Repeat("=", TableFixedWidth))
	fmt.Fprintf(w, "【总览】核心窗口 %02d:00–%02d:00 通透小时数（✅ 的小时数 / 有效小时数）\n",
		win.CoreStartHour, win.CoreEndHour)
	fmt.Fprintln(w, Repeat("=", TableFixedWidth))

	headerCells := make([]string, 0, len(nights)+1)
	headerCells = append(headerCells, Pad("点位", 12, AlignLeft))
	for _, n := range nights {

		short := n
		if len(n) > 5 {
			short = n[5:]
		}
		headerCells = append(headerCells, Pad(short, 12, AlignRight))
	}
	header := headerCells[0] + " " + strings.Join(headerCells[1:], " ")
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, Repeat("-", DispWidth(header)))

	bestOK, bestDesc := -1, ""
	hasCompare := len(compare) > 0
	for _, site := range sites {
		cells := []string{Pad(site.Name, 12, AlignLeft)}
		for _, night := range nights {
			core := coreRows(rows, site.Name, night, win, true)
			if len(core) == 0 {
				cells = append(cells, Pad("无数据", 12, AlignRight))
				continue
			}
			st := ComputeSiteNightStats(site.Name, night, rows, compare, cfg, [2]time.Time{})
			ok := st.OK
			if hasCompare {
				ok = st.CrossOK
			}
			cells = append(cells, Pad(fmt.Sprintf("%d/%d", ok, len(core)), 12, AlignRight))
			if ok > bestOK {
				bestOK = ok
				bestDesc = fmt.Sprintf("%s @ %s", site.Name, night)
			}
		}
		fmt.Fprintln(w, strings.Join(cells, " "))
	}
	fmt.Fprintln(w, Repeat("-", DispWidth(header)))
	if bestOK > 0 {
		fmt.Fprintf(w, "➜ 综合最优：%s（核心窗口 %dh 通透）\n", bestDesc, bestOK)
	} else {
		fmt.Fprintln(w, "➜ 有效预报范围内所有点位核心窗口均无通透小时；"+
			"若多为「无数据」请临近再跑，否则建议顺延或换区域。")
	}
	fmt.Fprintln(w)

	modelName := models
	if modelName == "" {
		modelName = "所选数值模式"
	}
	fmt.Fprintf(w, "说明：云底/云顶来自 %s 气压层剖面（%d–%dhPa）线性插值，属模式反演值，\n",
		modelName, maxPressure(), minPressure())
	fmt.Fprintln(w, "      分辨率受限于气压层间距（机位越高、可用层越少）；出发前请再跑一次以取最新预报。")
}

// PrintCrossModelSummary 在终端概览后输出双模型交叉对比摘要：
// 每站点 ICON/GFS/共识通透小时数 + 推荐标记，帮助快速识别「两模型都认」的可信窗口。
func PrintCrossModelSummary(w io.Writer, compare []model.ModelCompareRow, cfg config.Config) {
	_ = cfg
	fmt.Fprintln(w)
	fmt.Fprintln(w, Repeat("=", TableFixedWidth))
	fmt.Fprintln(w, "【双模型交叉对比】ICON seamless ↔ GFS seamless（两模型都认才可信）")
	fmt.Fprintln(w, Repeat("=", TableFixedWidth))

	type siteSum struct {
		site                  string
		iconOK, gfsOK, bothOK int
	}
	sums := make(map[string]*siteSum, 8)
	order := make([]string, 0, 8)
	for _, r := range compare {
		s := sums[r.Site]
		if s == nil {
			s = &siteSum{site: r.Site}
			sums[r.Site] = s
			order = append(order, r.Site)
		}
		if r.IconRating == model.RATING_OK {
			s.iconOK++
		}
		if r.GfsRating == model.RATING_OK {
			s.gfsOK++
		}
		if r.Consensus == model.ConsensusBothOK {
			s.bothOK++
		}
	}

	header := Pad("点位", 12, AlignLeft) + " " +
		Pad("ICON", 8, AlignRight) + " " +
		Pad("GFS", 8, AlignRight) + " " +
		Pad("共识", 8, AlignRight) + "  推荐"
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, Repeat("-", DispWidth(header)))
	for _, name := range order {
		s := sums[name]
		rec := "—"
		if s.bothOK >= 2 {
			rec = "✅ 共识窗口"
		} else if s.bothOK >= 1 {
			rec = "⚠️ 短时"
		}
		fmt.Fprintln(w,
			Pad(s.site, 12, AlignLeft)+" "+
				Pad(fmt.Sprintf("%d", s.iconOK), 8, AlignRight)+" "+
				Pad(fmt.Sprintf("%d", s.gfsOK), 8, AlignRight)+" "+
				Pad(fmt.Sprintf("%d", s.bothOK), 8, AlignRight)+"  "+rec)
	}
	fmt.Fprintln(w, Repeat("-", DispWidth(header)))
	fmt.Fprintln(w, "➜ 标 ✅ 的站点：ICON 与 GFS 在多个整点同时判通透，可信度最高，优先选。")
	fmt.Fprintln(w)
}

func InCoreWindow(hour int, w config.WindowConfig) bool {
	return hour >= w.CoreStartHour || hour <= w.CoreEndHour
}

func coreRows(rows []model.HourRow, site, night string,
	w config.WindowConfig, onlyValid bool) []model.HourRow {

	out := make([]model.HourRow, 0, 8)
	for _, r := range rows {
		if r.Site != site || r.Night != night || !InCoreWindow(r.Hour, w) {
			continue
		}
		if onlyValid && !r.HasData {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TimeISO < out[j].TimeISO })
	return out
}

func countRating(rows []model.HourRow, rating string) int {
	n := 0
	for _, r := range rows {
		if r.Rating == rating {
			n++
		}
	}
	return n
}

func filterRating(rows []model.HourRow, rating string) []model.HourRow {
	out := make([]model.HourRow, 0, len(rows))
	for _, r := range rows {
		if r.Rating == rating {
			out = append(out, r)
		}
	}
	return out
}

func SummariseSiteNight(siteName string, rows []model.HourRow,
	compare []model.ModelCompareRow, cfg config.Config) string {
	w := cfg.Window
	window := make([]model.HourRow, 0, 8)
	for _, r := range rows {
		if r.Site == siteName && InCoreWindow(r.Hour, w) {
			window = append(window, r)
		}
	}
	if len(window) == 0 {
		return ""
	}
	core := make([]model.HourRow, 0, len(window))
	for _, r := range window {
		if r.HasData {
			core = append(core, r)
		}
	}
	sort.SliceStable(core, func(i, j int) bool { return core[i].TimeISO < core[j].TimeISO })
	nMissing := len(window) - len(core)

	if len(core) == 0 {
		return fmt.Sprintf("%s %02d:00-%02d:00 无有效预报（%dh 超出模式时效）；❓ 请临近再跑",
			Pad(siteName, 10, AlignLeft), w.CoreStartHour, w.CoreEndHour, nMissing)
	}

	ok := filterRating(core, model.RATING_OK)
	warn := filterRating(core, model.RATING_WARN)
	bad := filterRating(core, model.RATING_BAD)

	hasSea := false
	for _, r := range core {
		if r.CloudSea == "有" {
			hasSea = true
			break
		}
	}

	crossOK := 0
	hasCompare := len(compare) > 0
	if hasCompare {
		for _, c := range compare {
			if c.Site == siteName && c.Consensus == model.ConsensusBothOK {
				crossOK++
			}
		}
	}
	effectiveOK := len(ok)
	if hasCompare {
		effectiveOK = crossOK
	}

	head := fmt.Sprintf("%s %02d:00-%02d:00 有效 %dh",
		Pad(siteName, 10, AlignLeft), w.CoreStartHour, w.CoreEndHour, len(core))
	if nMissing > 0 {
		head += fmt.Sprintf("(缺 %dh)", nMissing)
	}
	if hasCompare {
		head += fmt.Sprintf("：共识通透 %dh(ICON %dh) / 风险 %dh / 不宜 %dh", crossOK, len(ok), len(warn), len(bad))
	} else {
		head += fmt.Sprintf("：通透 %dh / 风险 %dh / 不宜 %dh", len(ok), len(warn), len(bad))
	}

	var detail, verdict string
	switch {
	case effectiveOK > 0:
		detail = "最佳连续窗口 " + LongestRun(core, model.RATING_OK)
		for _, r := range ok {
			if r.Relation.Valid && (r.Relation.V == model.REL_SEA_BELOW ||
				r.Relation.V == model.REL_SEA_BELOW_IN_CLOUD) {
				detail += "，其间出现云海在脚下"
				break
			}
		}
		if effectiveOK >= 3 {
			verdict = "✅ 可去"
		} else {
			verdict = "⚠️ 窗口偏短，需现场判断"
		}
	case len(warn) > 0:
		detail = "最长连续风险窗口 " + LongestRun(core, model.RATING_WARN)
		verdict = "⚠️ 有机会但不稳定"
	default:
		inCloud := 0
		seaInCloud := 0
		for _, r := range bad {
			if !r.Relation.Valid {
				continue
			}
			switch r.Relation.V {
			case model.REL_IN_CLOUD:
				inCloud++
			case model.REL_SEA_BELOW_IN_CLOUD:
				seaInCloud++
			}
		}
		switch {
		case seaInCloud > 0 && inCloud == 0:
			detail = fmt.Sprintf("其中 %dh 机位在云中（脚下有云海）", seaInCloud)
		case inCloud > 0:
			detail = fmt.Sprintf("其中 %dh 机位在云中", inCloud)
		default:
			detail = "全程头顶有云"
		}
		verdict = "🔴 建议放弃该点位"
	}
	// 云海提示与「主要状态」解耦：即便山顶起雾/降水把云海遮住，几何上有就补一句。
	if hasSea && !strings.Contains(detail, "云海") {
		detail += "；脚下有云海（机位下方），但被雾/云遮住时不可见"
	}
	return head + "；" + detail + "；" + verdict
}

func LongestRun(rows []model.HourRow, rating string) string {
	var best, cur []model.HourRow
	for _, r := range rows {
		if r.Rating == rating {

			if len(cur) > 0 {
				prevT, perr := time.ParseInLocation("2006-01-02T15:04", cur[len(cur)-1].TimeISO, time.UTC)
				curT, cerr := time.ParseInLocation("2006-01-02T15:04", r.TimeISO, time.UTC)
				if perr != nil || cerr != nil || curT.Sub(prevT) > time.Hour {
					cur = nil
				}
			}
			cur = append(cur, r)
			if len(cur) > len(best) {
				best = append([]model.HourRow(nil), cur...)
			}
		} else {
			cur = nil
		}
	}
	if len(best) == 0 {
		return "无"
	}
	first, err1 := time.ParseInLocation("2006-01-02T15:04", best[0].TimeISO, time.UTC)
	last, err2 := time.ParseInLocation("2006-01-02T15:04", best[len(best)-1].TimeISO, time.UTC)
	if err1 != nil || err2 != nil {
		return "无"
	}
	last = last.Add(time.Hour)
	return fmt.Sprintf("%s-%s（%dh）", first.Format("15:04"), last.Format("15:04"), len(best))
}

type NightAstroStats struct {
	Night        string
	MoonPhase    string
	MoonIllumPct float64
	MoonUpHours  int
	MoonDesc     string
	DarkHours    int
	GCMax        float64
}

func ComputeNightAstro(night string, sites []model.Site, cfg config.Config, utcOffsetSec int) NightAstroStats {
	st := NightAstroStats{Night: night, MoonPhase: "-"}
	if len(sites) == 0 {
		return st
	}
	var sumLat, sumLon float64
	for _, s := range sites {
		sumLat += s.Lat
		sumLon += s.Lon
	}
	lat := sumLat / float64(len(sites))
	lon := sumLon / float64(len(sites))

	base, err := time.ParseInLocation("2006-01-02T15:04", night+"T22:00", time.UTC)
	if err != nil {
		return st
	}
	utcOffset := utcOffsetSec
	darkSunAlt := cfg.Thresh.AstroDarkSunAlt

	darkHours, moonUpHours := 0, 0
	gcMax := 0.0
	for h := 0; h <= 8; h++ {
		a := astro.Compute(base.Add(time.Duration(h)*time.Hour), utcOffset, lat, lon, darkSunAlt)
		if a.AstroDark {
			darkHours++
		}
		if a.MoonAlt > 0 {
			moonUpHours++
		}
		if h == 0 || a.GCAlt > gcMax {
			gcMax = a.GCAlt
		}
	}
	midnight := astro.Compute(base.Add(2*time.Hour), utcOffset, lat, lon, darkSunAlt)
	illumPct := midnight.MoonIllum * 100.0

	var moonDesc string
	switch {
	case moonUpHours > 0 && midnight.MoonIllum >= cfg.Thresh.MoonBrightIllum:
		moonDesc = fmt.Sprintf("%s %s%%，夜间在地平线上 %dh（有月光干扰）",
			midnight.MoonPhaseName, FormatFixed(illumPct, 0), moonUpHours)
	case moonUpHours > 0:
		moonDesc = fmt.Sprintf("%s %s%%，月光影响小",
			midnight.MoonPhaseName, FormatFixed(illumPct, 0))
	default:
		moonDesc = fmt.Sprintf("%s %s%%，整夜月亮在地平线下（无月夜）",
			midnight.MoonPhaseName, FormatFixed(illumPct, 0))
	}

	return NightAstroStats{
		Night:        night,
		MoonPhase:    midnight.MoonPhaseName,
		MoonIllumPct: illumPct,
		MoonUpHours:  moonUpHours,
		MoonDesc:     moonDesc,
		DarkHours:    darkHours,
		GCMax:        gcMax,
	}
}

func nightAstroLine(night string, sites []model.Site, cfg config.Config, utcOffsetSec int) string {
	st := ComputeNightAstro(night, sites, cfg, utcOffsetSec)
	return fmt.Sprintf("[天文·近似] 月相 %s；天文暗夜 %dh；银心最高 %s°",
		st.MoonDesc, st.DarkHours, FormatFixed(st.GCMax, 0))
}

// pressureLevels 终端报告里用于输出的气压层清单。
// 必须与 internal/api.PressureLevels / internal/profile.PressureLevels 保持一致，
// 否则终端报告与 Markdown 报告中的云层剖面会对不上。
var pressureLevels = [...]int{1000, 975, 950, 925, 900, 875, 850, 825, 800, 750, 700}

func maxPressure() int {
	m := pressureLevels[0]
	for _, p := range pressureLevels {
		if p > m {
			m = p
		}
	}
	return m
}

func minPressure() int {
	m := pressureLevels[0]
	for _, p := range pressureLevels {
		if p < m {
			m = p
		}
	}
	return m
}
