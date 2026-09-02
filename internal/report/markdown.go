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
		site                                              string
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

	// InCloudHours 机位在云中的时次数（REL_IN_CLOUD 普通埋云 / REL_SEA_BELOW_IN_CLOUD
	// 高山云海淹没机位）。用于「主要状态」列在众数为全层无云时加注限定，避免
	// "全层无云"被误读为整夜干净——这少数时次正是把结论拉离「可去」的致命短板。
	InCloudHours int

	// CloudSea 夜间级「日出窗云海」聚合：有 / 有（被山顶雾/降水遮蔽） / 辐射雾 / 无。
	// 「辐射雾」档：日出窗内 note 含"辐射雾"字样（贴地+静风+晴夜少云的雾场景）。
	// 优先级：云海可见 > 辐射雾遮蔽 > 其他遮蔽 > 无。
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

	// 统计机位在云中的时次（REL_IN_CLOUD 普通埋云 / REL_SEA_BELOW_IN_CLOUD 高山云海
	// 淹没机位），供「主要状态」列在众数为全层无云时加注限定。
	for _, r := range valid {
		if !r.Relation.Valid {
			continue
		}
		if r.Relation.V == model.REL_IN_CLOUD || r.Relation.V == model.REL_SEA_BELOW_IN_CLOUD {
			st.InCloudHours++
		}
	}

	effectiveOK := st.OK
	if hasCompare {
		effectiveOK = st.CrossOK
	}
	if effectiveOK > 0 {
		st.BestWindow = LongestRun(valid, model.RATING_OK)
	}

	st.MainReason = mainReason(valid)

	// 日出窗云海聚合：仅统计落在「日出拍摄窗口」内的有效时次，
	// 反映日出前后机位下方云海状况，而非整夜。窗口为零值（未提供）时退回全夜统计。
	// 与「主要状态/主要诱因」解耦，单独列示。
	// 优先级：可见云海+辐射雾兼具 > 云海可见 > 辐射雾遮蔽 > 其他遮蔽 > 无。
	// 「辐射雾」档：note 含"辐射雾"（贴地+静风+晴夜少云的雾场景，由 profile
	// applyFogVeto 的 fogKindText 或辐射雾豁免分支写入）时计入——告诉用户
	// 日出窗是辐射雾贴地，日出后大概率消散可守候破云。
	// 「辐射雾（云海）」档：窗口内既出现 OK 级可见云海时次、又出现辐射雾时次，
	// 说明脚下云海与贴地辐射雾同框——静风、日出可守候破云与云海，最该守候的题材。
	seaTotal, seaVisible, radFogHours := 0, 0, 0
	var radBases, radTops []float64 // 窗口内辐射雾时次相对机位的云底/云顶 AGL（米）
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
		if strings.Contains(r.Note, "辐射雾") {
			radFogHours++
			// 仅聚合"雾顶在机位之上(tops>=0)"的辐射雾时次——即雾覆盖/包裹机位、
			// 你身处雾中的几何，这正是拍辐射雾要判断的高度。排除纯"云海在脚下"
			// (雾全在机下, tops<0) 时次，避免其厚云层几何把括号高度拉偏、与主要
			// 状态"机位在云中"给出的机上高度矛盾。
			if r.CloudTopAGL.Valid && r.CloudTopAGL.V >= 0 {
				if r.CloudBaseAGL.Valid {
					radBases = append(radBases, r.CloudBaseAGL.V)
				}
				radTops = append(radTops, r.CloudTopAGL.V)
			}
		}
	}
	switch {
	case seaVisible > 0 && radFogHours > 0:
		st.CloudSea = radFogLabel("云海", radBases, radTops)
	case seaVisible > 0:
		st.CloudSea = "有"
	case radFogHours > 0:
		st.CloudSea = radFogLabel("", radBases, radTops)
	case seaTotal > 0:
		st.CloudSea = "有（被山顶雾/降水遮蔽）"
	default:
		st.CloudSea = "无"
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
	// r.Time 以 UTC 承载本地墙钟（见 internal/api/response.go），
	// sunriseWin 以本地时区(+8 等)承载同一本地墙钟；二者时区不同，
	// 直接比较绝对瞬间会整体误判。统一剥去时区、保留墙钟再比。
	tc := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
	w0 := time.Date(w[0].Year(), w[0].Month(), w[0].Day(), w[0].Hour(), w[0].Minute(), 0, 0, time.UTC)
	w1 := time.Date(w[1].Year(), w[1].Month(), w[1].Day(), w[1].Hour(), w[1].Minute(), 0, 0, time.UTC)
	return tc.After(w0) && tc.Before(w1)
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

// RelationLabel 返回「主要状态」列的展示文案。
// DominantRelation 是整夜几何关系的众数（出现最多那档）。当众数为 REL_CLEAR（全层无云）
// 但夜里有 N 个时次机位仍在云中（REL_IN_CLOUD / REL_SEA_BELOW_IN_CLOUD）时，加注限定，
// 避免"全层无云"被误读为整夜干净——这少数时次正是把结论拉离「可去」的致命短板。
func RelationLabel(st SiteNightStats) string {
	if st.DominantRelation == "" {
		return MissingCell
	}
	label, ok := model.RelLabels[st.DominantRelation]
	if !ok {
		return MissingCell
	}
	if st.DominantRelation == model.REL_CLEAR && st.InCloudHours > 0 {
		return fmt.Sprintf("全层无云（多数时次，%d 时次机位在云中）", st.InCloudHours)
	}
	return label
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

// radFogLabel 为「日出窗云海」列的辐射雾档生成标签。
// extra 用于额外上下文（如"云海"表示脚下云海与辐射雾同框）；
// bases/tops 为窗口内辐射雾时次相对机位的云底/云顶 AGL（米，负=机下、正=机上），
// 用于在括号里给出辐射雾层相对机位的高度范围，便于判断无人机能否飞出雾顶、
// 起降是否在雾中。无有效高度数据时退回纯"辐射雾"。
func radFogLabel(extra string, bases, tops []float64) string {
	span := radFogSpan(bases, tops)
	parts := make([]string, 0, 2)
	if extra != "" {
		parts = append(parts, extra)
	}
	if span != "" {
		parts = append(parts, span)
	}
	if len(parts) == 0 {
		return "辐射雾"
	}
	return "辐射雾（" + strings.Join(parts, "·") + "）"
}

// radFogSpan 据窗口内辐射雾时次的云底/云顶 AGL，给出雾层相对机位的高度范围文字。
// 返回空串表示无有效高度数据（不附加括号）。
func radFogSpan(bases, tops []float64) string {
	var lo, hi float64
	have := false
	for _, v := range bases {
		if !have {
			lo, hi = v, v
			have = true
			continue
		}
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	for _, v := range tops {
		if !have {
			lo, hi = v, v
			have = true
			continue
		}
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	if !have {
		return ""
	}
	// lo/hi 相对机位 AGL：负=机下，正=机上。
	switch {
	case lo < 0 && hi > 0:
		return fmt.Sprintf("机下%.0fm·机上%.0fm", -lo, hi)
	case hi <= 0:
		// 全在机下：lo、hi 均≤0，lo 更负。范围由浅(-hi)到深(-lo)。
		return fmt.Sprintf("机下%.0f~%.0fm", -hi, -lo)
	default:
		// 全在机上：lo≥0。
		return fmt.Sprintf("机上%.0f~%.0fm", lo, hi)
	}
}
