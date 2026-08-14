package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/astro"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

const (
	ReportPrefix = "astro_report"

	ReportSuffix = ".md"
)

func ReportFilename(meta model.ReportMeta) string {
	if meta.Peak.Valid && meta.Peak.V != "" {
		return ReportPrefix + "_peak-" + meta.Peak.V + ReportSuffix
	}
	return ReportPrefix + "_" + meta.Start + "_" + meta.End + ReportSuffix
}

func ExportFilename(meta model.ReportMeta, suffix string) string {
	name := ReportFilename(meta)
	return strings.TrimSuffix(name, ReportSuffix) + suffix
}

func BuildMarkdownReport(rows []model.HourRow, compare []model.ModelCompareRow, meta model.ReportMeta,
	cfg config.Config) string {

	lines := make([]string, 0, 256)
	lines = append(lines, mdHeadSection(meta, cfg)...)

	hasAny := false
	for i := range rows {
		if rows[i].HasData {
			hasAny = true
			break
		}
	}
	if !hasAny {
		lines = append(lines, mdNoForecastBanner()...)
	}

	lines = append(lines, mdNightlySection(rows, compare, meta, cfg)...)
	lines = append(lines, mdWeightSection(cfg)...)
	lines = append(lines, mdDetailSection(rows, compare, meta, cfg)...)
	lines = append(lines, mdFieldLegendSection()...)

	if len(compare) > 0 {
		lines = append(lines, mdCrossModelSection(compare, cfg)...)
	}

	source := meta.Source
	if source == "" {
		source = "Open-Meteo"
	}
	lines = append(lines,
		"---",
		"",
		fmt.Sprintf("*由 `astro-mountain` 自动生成 · 数据源 %s · 模式 `%s`。%s*",
			source, meta.Models, meta.Disclaimer),
		"",
	)
	return strings.Join(lines, "\n")
}

func WriteMarkdownReport(rows []model.HourRow, compare []model.ModelCompareRow, meta model.ReportMeta,
	cfg config.Config, outDir string) (string, error) {

	if outDir == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("创建报告目录 %s 失败：%w", outDir, err)
	}
	path := filepath.Join(outDir, ReportFilename(meta))
	text := BuildMarkdownReport(rows, compare, meta, cfg)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("写入报告 %s 失败：%w", path, err)
	}
	return path, nil
}

// mdCrossModelSection 渲染双模型交叉对比章节：先给每站点级汇总（ICON/GFS/共识通透小时
// 数 + 推荐标记），再给每站点逐小时对比表（时间 / ICON / GFS / 共识）。
// 共识仅当两模型都判通透（RATING_OK）才算可信窗口。
func mdCrossModelSection(compare []model.ModelCompareRow, cfg config.Config) []string {
	_ = cfg
	lines := make([]string, 0, 64)
	lines = append(lines,
		"## 双模型交叉对比（ICON seamless ↔ GFS seamless）",
		"",
		"> 单模型 ICON 在华东山地常偏乐观。下表把 ICON 与 GFS 逐小时配对：两模型都认的通透窗口才可信；",
		"> 仅一个模型认的时段标 ⚠️ 分歧，需谨慎。",
		"",
	)

	type siteSum struct {
		site                              string
		iconOK, gfsOK, bothOK, iconOnly, gfsOnly, bothBad int
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
		switch r.Consensus {
		case model.ConsensusBothOK:
			s.bothOK++
		case model.ConsensusBothBad:
			s.bothBad++
		case model.ConsensusIconOnly:
			s.iconOnly++
		case model.ConsensusGfsOnly:
			s.gfsOnly++
		}
	}

	headers := []string{"站点", "ICON 通透h", "GFS 通透h", "共识通透h", "推荐"}
	rows := make([][]string, 0, len(order))
	for _, name := range order {
		s := sums[name]
		rec := "—"
		switch {
		case s.bothOK >= 2:
			rec = "✅ 双模型共识窗口"
		case s.bothOK >= 1:
			rec = "⚠️ 仅短时共识"
		}
		rows = append(rows, []string{
			s.site,
			fmt.Sprintf("%d", s.iconOK),
			fmt.Sprintf("%d", s.gfsOK),
			fmt.Sprintf("%d", s.bothOK),
			rec,
		})
	}
	lines = append(lines, MDTable(headers, rows)...)
	lines = append(lines, "")

	for _, name := range order {
		lines = append(lines, fmt.Sprintf("### %s · 逐小时对比", name), "")
		hours := make([]model.ModelCompareRow, 0, 16)
		for _, r := range compare {
			if r.Site == name {
				hours = append(hours, r)
			}
		}
		hrows := make([][]string, 0, len(hours))
		for _, h := range hours {
			hrows = append(hrows, []string{
				h.TimeShort,
				ratingShort(h.IconRating),
				ratingShort(h.GfsRating),
				consensusText(h.Consensus),
			})
		}
		lines = append(lines, MDTable([]string{"时间", "ICON", "GFS", "共识"}, hrows)...)
		lines = append(lines, "")
	}
	return lines
}

func ratingShort(r string) string {
	switch r {
	case model.RATING_OK:
		return "通透"
	case model.RATING_WARN:
		return "风险"
	case model.RATING_BAD:
		return "不宜"
	default:
		return "缺测"
	}
}

func consensusText(c string) string {
	switch c {
	case model.ConsensusBothOK:
		return "✅ 两模型都通透"
	case model.ConsensusBothBad:
		return "❌ 两模型都不宜"
	case model.ConsensusIconOnly:
		return "⚠️ 仅 ICON 通透"
	case model.ConsensusGfsOnly:
		return "⚠️ 仅 GFS 通透"
	default:
		return "— 缺测"
	}
}

func mdCell(value string) string {
	if value == "" {
		return MissingCell
	}
	text := strings.ReplaceAll(value, "|", "\\|")
	return strings.Join(strings.Fields(text), " ")
}

func MDTable(headers []string, rows [][]string) []string {
	lines := make([]string, 0, len(rows)+2)

	head := make([]string, 0, len(headers))
	for _, h := range headers {
		head = append(head, mdCell(h))
	}
	lines = append(lines, "| "+strings.Join(head, " | ")+" |")

	sep := make([]string, 0, len(headers))
	for range headers {
		sep = append(sep, "---")
	}
	lines = append(lines, "|"+strings.Join(sep, "|")+"|")

	for _, row := range rows {
		cells := make([]string, 0, len(row))
		for _, c := range row {
			cells = append(cells, mdCell(c))
		}
		lines = append(lines, "| "+strings.Join(cells, " | ")+" |")
	}
	return lines
}

type SiteNightStats struct {
	Site    string
	Night   string
	Planned int
	Valid   int
	Missing int

	OK      int
	Warn    int
	Bad     int
	CrossOK int // 双模型交叉时两模型都判 RATING_OK 的小时数

	BestWindow string

	BaseAGLMin model.OptFloat
	BaseAGLMax model.OptFloat
	TopAGLMin  model.OptFloat
	TopAGLMax  model.OptFloat

	DominantRelation string
	Verdict          string
	MainReason       string

	// CloudSea 夜间级「有无云海」聚合：有 / 有（被山顶雾/降水遮蔽） / 无。
	CloudSea string
}

func ComputeSiteNightStats(siteName, night string, rows []model.HourRow,
	compare []model.ModelCompareRow, cfg config.Config, sunriseWin [2]time.Time) SiteNightStats {

	st := SiteNightStats{Site: siteName, Night: night, BestWindow: "无"}

	core := make([]model.HourRow, 0, 8)
	for _, r := range rows {
		if r.Site == siteName && r.Night == night && InCoreWindow(r.Hour, cfg.Window) {
			core = append(core, r)
		}
	}
	valid := make([]model.HourRow, 0, len(core))
	for _, r := range core {
		if r.HasData {
			valid = append(valid, r)
		}
	}
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].TimeISO < valid[j].TimeISO })

	st.Planned = len(core)
	st.Valid = len(valid)
	st.Missing = len(core) - len(valid)

	ok := filterRating(valid, model.RATING_OK)
	warn := filterRating(valid, model.RATING_WARN)
	bad := filterRating(valid, model.RATING_BAD)
	st.OK, st.Warn, st.Bad = len(ok), len(warn), len(bad)

	// 双模型交叉：统计两模型都判通透的整点数
	hasCompare := len(compare) > 0
	if hasCompare {
		for _, c := range compare {
			if c.Site != siteName || c.Night != night {
				continue
			}
			if !InCoreWindow(c.Hour, cfg.Window) {
				continue
			}
			if c.Consensus == model.ConsensusBothOK {
				st.CrossOK++
			}
		}
	}

	st.DominantRelation = dominantRelation(valid)
	if st.DominantRelation != "" {
		bases := make([]float64, 0, len(valid))
		tops := make([]float64, 0, len(valid))
		for _, r := range valid {
			if !r.Relation.Valid || r.Relation.V != st.DominantRelation {
				continue
			}
			if r.CloudBaseAGL.Valid {
				bases = append(bases, r.CloudBaseAGL.V)
			}
			if r.CloudTopAGL.Valid {
				tops = append(tops, r.CloudTopAGL.V)
			}
		}
		st.BaseAGLMin, st.BaseAGLMax = minMax(bases)
		st.TopAGLMin, st.TopAGLMax = minMax(tops)
	}

	effectiveOK := st.OK
	if hasCompare {
		effectiveOK = st.CrossOK
	}
	if effectiveOK > 0 {
		st.BestWindow = LongestRun(valid, model.RATING_OK)
	}

	st.MainReason = mainReason(valid)

	// 有无云海（日出窗口聚合）：仅统计落在「日出拍摄窗口」内的有效时次，
	// 反映日出前后机位下方云海状况，而非整夜。窗口为零值（未提供）时退回全夜统计。
	// 若所有云海时次都被山顶雾/降水压成不宜，则标注「被遮蔽」，与「主要状态」解耦。
	seaTotal, seaVisible := 0, 0
	for _, r := range valid {
		if !inSunriseWindow(r.Time, sunriseWin) {
			continue
		}
		if r.CloudSea == "有" {
			seaTotal++
			if r.Rating == model.RATING_OK {
				seaVisible++
			}
		}
	}
	switch {
	case seaTotal == 0:
		st.CloudSea = "无"
	case seaVisible > 0:
		st.CloudSea = "有"
	default:
		st.CloudSea = "有（被山顶雾/降水遮蔽）"
	}

	switch {
	case len(valid) == 0:
		st.Verdict = "❓ 无有效预报，请临近再跑"
	case effectiveOK >= 3:
		st.Verdict = "✅ 可去"
	case effectiveOK > 0:
		st.Verdict = "⚠️ 窗口偏短，需现场判断"
	case len(warn) > 0:
		st.Verdict = "⚠️ 有机会但不稳定"
	default:
		st.Verdict = "🔴 建议放弃该点位"
	}
	return st
}

// inSunriseWindow 判断时刻 t 是否落在日出拍摄窗口内。
// 窗口为零值（未提供日出时刻）时返回 true，即退回「全夜计入」，供不关心窗口的调用方使用。
func inSunriseWindow(t time.Time, w [2]time.Time) bool {
	if w[0].IsZero() && w[1].IsZero() {
		return true
	}
	return t.After(w[0]) && t.Before(w[1])
}

// SunriseWindowForNight 计算点位 site 在观测夜 night 对应的「日出拍摄窗口」。
//
// night 为傍晚日期（如 2026-08-12），日出发生在其次日清晨，故以 night+1 作为 morningDate
// 传入 astro.SunriseTime。窗口 = [日出 - beforeMin, 日出 + afterMin]，前后余量由 cfg 控制。
// 返回 [start, end] 与 ok；ok=false 表示未求得日出（如极地），调用方应退回全夜统计或标记无数据。
func SunriseWindowForNight(site model.Site, night string, utcOffsetSec int, cfg config.Config) ([2]time.Time, bool) {
	y, m, d, err := parseNightDate(night)
	if err != nil {
		return [2]time.Time{}, false
	}
	morning := time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC)
	sr, ok := astro.SunriseTime(site.Lat, site.Lon, utcOffsetSec, morning)
	if !ok {
		return [2]time.Time{}, false
	}
	before := cfg.Window.SunriseWindowBeforeMin
	after := cfg.Window.SunriseWindowAfterMin
	if before < 0 {
		before = 0
	}
	if after < 0 {
		after = 0
	}
	return [2]time.Time{
		sr.Add(-time.Duration(before) * time.Minute),
		sr.Add(time.Duration(after) * time.Minute),
	}, true
}

// parseNightDate 把 "2006-01-02" 形式的观测夜字符串解析为年月日。
func parseNightDate(s string) (year int, month time.Month, day int, err error) {
	t, e := time.Parse("2006-01-02", s)
	if e != nil {
		return 0, 0, 0, e
	}
	return t.Year(), t.Month(), t.Day(), nil
}

// parseHourShort 把 "22:00" 或 "2026-08-12T22:00" 里的整点解析出来。
func dominantRelation(rows []model.HourRow) string {
	counts := make(map[string]int, 5)
	order := make([]string, 0, 5)
	for _, r := range rows {

		if !r.Relation.Valid || r.Relation.V == "" {
			continue
		}
		if _, seen := counts[r.Relation.V]; !seen {
			order = append(order, r.Relation.V)
		}
		counts[r.Relation.V]++
	}
	best, bestN := "", 0
	for _, rel := range order {
		if counts[rel] > bestN {
			best, bestN = rel, counts[rel]
		}
	}
	return best
}

func reasonCategory(note string) string {
	if note == "" {
		return ""
	}
	switch {

	case strings.Contains(note, "雷暴") || strings.Contains(note, "降水"):
		return "降水 / 雷暴"

	case strings.Contains(note, "辐射雾（") || strings.Contains(note, "平流雾/低云压顶（") ||
		strings.Contains(note, "，有雾"):
		return "浓雾（能见度<1000m）"
	case strings.Contains(note, "机位在云中"):
		return "机位在云中"
	case strings.Contains(note, "成片遮挡"):
		return "头顶厚云（云量≥70%）"
	case strings.Contains(note, "轻雾/霾") || strings.Contains(note, "起雾风险"):
		return "轻雾/霾（1000–5000m）"
	case strings.Contains(note, "中云量") && strings.Contains(note, "（3–8km"):
		return "中云盖顶（3–8km）"
	case strings.Contains(note, "高云量") && strings.Contains(note, "（8km 以上"):
		return "高云洗天（8km+）"
	case strings.Contains(note, "云底在头顶"):
		return "头顶薄云（云量40–70%）"

	case strings.Contains(note, "镜头结露") || strings.Contains(note, "LCL≈"):
		return "结露 / LCL 风险"
	}
	return ""
}

func mainReason(rows []model.HourRow) string {
	pool := rows[:0:0]
	for _, r := range rows {
		if r.Rating == model.RATING_BAD {
			pool = append(pool, r)
		}
	}
	if len(pool) == 0 {
		for _, r := range rows {
			if r.Rating == model.RATING_WARN {
				pool = append(pool, r)
			}
		}
	}
	if len(pool) == 0 {
		return ""
	}

	order := []string{
		"降水 / 雷暴",
		"浓雾（能见度<1000m）",
		"机位在云中",
		"头顶厚云（云量≥70%）",
		"轻雾/霾（1000–5000m）",
		"中云盖顶（3–8km）",
		"高云洗天（8km+）",
		"头顶薄云（云量40–70%）",
		"结露 / LCL 风险",
	}
	counts := make(map[string]int, len(order))
	for _, r := range pool {
		if cat := reasonCategory(r.Note); cat != "" {
			counts[cat]++
		}
	}
	if len(counts) == 0 {
		return ""
	}
	best, bestN := "", 0
	for _, cat := range order {
		n := counts[cat]
		if n == 0 {
			continue
		}
		if n > bestN || (n == bestN && best == "") {
			best, bestN = cat, n
		}
	}
	return best
}

func minMax(vs []float64) (model.OptFloat, model.OptFloat) {
	if len(vs) == 0 {
		return model.Missing(), model.Missing()
	}
	lo, hi := vs[0], vs[0]
	for _, v := range vs[1:] {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return model.Num(lo), model.Num(hi)
}

func fmtRange(low, high model.OptFloat, hasValid bool) string {
	if !low.Valid || !high.Valid {
		if hasValid {
			return "-（未反演到云层）"
		}
		return "-"
	}
	if low.V == high.V {
		return FormatG(low.V)
	}
	return FormatG(low.V) + " ~ " + FormatG(high.V)
}
