package report

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
)

const (
	MetaSourceOpenMeteo = "Open-Meteo free API"

	MetaSourceTomorrow = "Tomorrow.io"

	MetaSourceMeteoblue = "Meteoblue"
)

const TomorrowTrackLabel = "Tomorrow.io（B 轨）"

func IsTomorrowSource(source string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(source)), "tomorrow")
}

const (
	tomorrowISOLayout   = "2006-01-02T15:04"
	tomorrowShortLayout = "01-02 15:04"
	tomorrowUTCLayout   = "2006-01-02T15:04Z"
	tomorrowDateLayout  = "2006-01-02"
	tomorrowStampLayout = "2006-01-02 15:04"
)

func tomorrowLocalISO(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(tomorrowISOLayout)
}

func tomorrowLocalShort(t time.Time) string {
	if t.IsZero() {
		return MissingCell
	}
	return t.Format(tomorrowShortLayout)
}

func tomorrowUTCISO(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(tomorrowUTCLayout)
}

func TomorrowNightID(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Add(-12 * time.Hour).Format(tomorrowDateLayout)
}

func TomorrowNightKeys(tracks []*dualtrack.TrackResult) []string {
	seen := make(map[string]bool, 8)
	out := make([]string, 0, 8)
	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		for _, v := range tr.Rows {
			night := TomorrowNightID(v.TimeLocal)
			if night == "" || seen[night] {
				continue
			}
			seen[night] = true
			out = append(out, night)
		}
	}
	sort.Strings(out)
	return out
}

func TomorrowSiteIDs(tracks []*dualtrack.TrackResult) []string {
	seen := make(map[string]bool, len(tracks))
	out := make([]string, 0, len(tracks))
	for _, tr := range tracks {
		if tr == nil || tr.SiteID == "" || seen[tr.SiteID] {
			continue
		}
		seen[tr.SiteID] = true
		out = append(out, tr.SiteID)
	}
	return out
}

func TomorrowNoDataTag(v dualtrack.HourVerdict) string {
	if v.NoDataReason == dualtrack.NoDataNone {
		return ""
	}
	label := v.NoDataReason.Label()
	if label == "" {
		return ""
	}
	return "[" + label + "]"
}

func tomorrowNoteCell(v dualtrack.HourVerdict) string {
	tag := TomorrowNoDataTag(v)
	switch {
	case tag == "" && v.Note == "":
		return MissingCell
	case tag == "":
		return v.Note
	case v.Note == "":
		return tag
	default:
		return tag + " " + v.Note
	}
}

func tomorrowRelCell(rel string) string {
	if rel == "" {
		return MissingCell
	}
	if label, ok := model.RelLabels[rel]; ok {
		return label
	}
	return rel
}

var TableColsTomorrow = []TableCol{
	{"点位", 10, AlignLeft},
	{"时间(北京)", 12, AlignLeft},
	{"云底相对机位", 13, AlignRight},
	{"云底AGL(模式)", 14, AlignRight},
	{"ΔH", 7, AlignRight},
	{"保真度", 16, AlignLeft},
	{"评级", 8, AlignLeft},
	{"判断说明", 0, AlignLeft},
}

var TomorrowTableFixedWidth = func() int {
	sum := 0
	for _, c := range TableColsTomorrow {
		sum += c.Width
	}
	return sum + len(TableColsTomorrow) - 1
}()

func fmtTomorrowRowCells(siteID string, v dualtrack.HourVerdict) []string {
	return []string{
		siteID,
		tomorrowLocalShort(v.TimeLocal),
		FmtInt(v.CloudBaseAboveSite),
		FmtInt(v.CloudBaseAGLM),
		FmtInt(v.DeltaH),
		v.TerrainFidelity.Label(),
		v.Rating,
		tomorrowNoteCell(v),
	}
}

func renderTomorrowLine(cells []string) string {
	out := make([]string, 0, len(TableColsTomorrow))
	for i, col := range TableColsTomorrow {
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

func PrintTomorrowHeader(w io.Writer, meta model.ReportMeta, cfg config.Config) {
	t, win := cfg.Thresh, cfg.Window
	line := Repeat("=", TomorrowTableFixedWidth)
	dash := Repeat("-", TomorrowTableFixedWidth)

	fmt.Fprintln(w, line)
	fmt.Fprintln(w, "山地星空 / 流星雨 低云海拔评估  v2   （Tomorrow.io 云底直接产品 + 模式地形订正）")
	fmt.Fprintln(w, dash)
	fmt.Fprintf(w, "数据源     : %s   (免费配额受限，超额当轮判无数据)\n", TomorrowTrackLabel)

	fmt.Fprintf(w, "查询范围   : %s ~ %s   时区 %s\n",
		meta.Start, meta.End,
		fmt.Sprintf("UTC%+0d", int(math.Round(meta.UTCOffsetHours))))
	fmt.Fprintf(w, "观测夜     : %s\n", orDash(meta.NightsDesc))
	fmt.Fprintf(w, "夜间窗口   : %02d:00 ~ %02d:00（%s，跨零点）\n",
		win.NightStartHour, win.NightEndHour,
		fmt.Sprintf("UTC%+0d", int(math.Round(meta.UTCOffsetHours))))

	names := make([]string, 0, len(meta.Sites))
	for _, s := range meta.Sites {
		names = append(names, fmt.Sprintf("%s(%sm)", s.Name, FormatG(s.Alt)))
	}
	fmt.Fprintf(w, "点位(%d) : %s\n", len(meta.Sites), strings.Join(names, "  "))

	fmt.Fprintf(w, "判据       : 云底相对机位高度 + 能见度<%sm 判雾；云量/能见度阈值与 A 轨同源\n",
		FormatFixed(t.FogVisibilityM, 0))
	fmt.Fprintln(w, "列义       : 云底相对机位=已用模式地形高度订正到真实机位；"+
		"云底AGL(模式)=接口原值（相对模式地形）")
	fmt.Fprintln(w, "ΔH         : 模式地形高度 − 机位海拔。负值越大说明模式把这个山头削得越平，"+
		"该点位 B 轨判定力越弱")
	fmt.Fprintln(w, "能力边界   : 本轨无云顶字段 —— 脚下有没有云海一律不可判，"+
		"云海判定请改用 --source openmeteo")
	fmt.Fprintln(w, line)
}

type tomorrowNightRow struct {
	SiteID  string
	Verdict dualtrack.HourVerdict
}

func tomorrowRowsOfNight(tracks []*dualtrack.TrackResult, night string) []tomorrowNightRow {
	out := make([]tomorrowNightRow, 0, 64)
	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		for _, v := range tr.Rows {
			if night != "" && TomorrowNightID(v.TimeLocal) != night {
				continue
			}
			out = append(out, tomorrowNightRow{SiteID: tr.SiteID, Verdict: v})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ti := tomorrowLocalISO(out[i].Verdict.TimeLocal)
		tj := tomorrowLocalISO(out[j].Verdict.TimeLocal)
		if ti != tj {
			return ti < tj
		}
		return out[i].SiteID < out[j].SiteID
	})
	return out
}

func PrintTomorrowNightBlock(w io.Writer, night string,
	tracks []*dualtrack.TrackResult, cfg config.Config) {

	rows := tomorrowRowsOfNight(tracks, night)

	fmt.Fprintln(w)
	fmt.Fprintln(w, Repeat("#", TomorrowTableFixedWidth))
	fmt.Fprintf(w, "■ 观测夜 %s（%s 22:00 → 次日 06:00）   数据源 %s\n",
		night, night, TomorrowTrackLabel)
	fmt.Fprintln(w, Repeat("#", TomorrowTableFixedWidth))

	if len(rows) == 0 {
		fmt.Fprintln(w, "  ⛔ 该夜 B 轨没有任何时次（可能整轮配额耗尽或超出 Tomorrow.io 预报窗）。")
		return
	}

	titles := make([]string, 0, len(TableColsTomorrow))
	for _, c := range TableColsTomorrow {
		titles = append(titles, c.Title)
	}
	fmt.Fprintln(w, renderTomorrowLine(titles))
	fmt.Fprintln(w, Repeat("-", TomorrowTableFixedWidth))
	for _, r := range rows {
		fmt.Fprintln(w, renderTomorrowLine(fmtTomorrowRowCells(r.SiteID, r.Verdict)))
	}

	fmt.Fprintln(w, Repeat("-", TomorrowTableFixedWidth))
	fmt.Fprintf(w, "【%s 夜 · 各点位结论（%s）】\n", night, TomorrowTrackLabel)
	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		st := ComputeTomorrowSiteNightStats(tr, night, cfg)
		if st.Planned == 0 {
			continue
		}
		fmt.Fprintln(w, "  "+st.Line())
	}
}

func PrintTomorrowOverview(w io.Writer, tracks []*dualtrack.TrackResult,
	nights []string, cfg config.Config) {

	win := cfg.Window
	fmt.Fprintln(w)
	fmt.Fprintln(w, Repeat("=", TomorrowTableFixedWidth))
	fmt.Fprintf(w, "【总览 · %s】核心窗口 %02d:00–%02d:00 通透小时数（✅ 的小时数 / 可判小时数）\n",
		TomorrowTrackLabel, win.CoreStartHour, win.CoreEndHour)
	fmt.Fprintln(w, Repeat("=", TomorrowTableFixedWidth))

	headerCells := make([]string, 0, len(nights)+1)
	headerCells = append(headerCells, Pad("点位", 12, AlignLeft))
	for _, n := range nights {
		short := n
		if len(n) > 5 {
			short = n[5:]
		}
		headerCells = append(headerCells, Pad(short, 12, AlignRight))
	}
	header := strings.Join(headerCells, " ")
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, Repeat("-", DispWidth(header)))

	bestOK, bestDesc := -1, ""
	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		cells := []string{Pad(tr.SiteID, 12, AlignLeft)}
		for _, night := range nights {
			st := ComputeTomorrowSiteNightStats(tr, night, cfg)
			if st.Judgeable == 0 {
				cells = append(cells, Pad("无数据", 12, AlignRight))
				continue
			}
			cells = append(cells, Pad(fmt.Sprintf("%d/%d", st.OK, st.Judgeable), 12, AlignRight))
			if st.OK > bestOK {
				bestOK = st.OK
				bestDesc = fmt.Sprintf("%s @ %s", tr.SiteID, night)
			}
		}
		fmt.Fprintln(w, strings.Join(cells, " "))
	}
	fmt.Fprintln(w, Repeat("-", DispWidth(header)))
	if bestOK > 0 {
		fmt.Fprintf(w, "➜ 综合最优：%s（核心窗口 %dh 通透）\n", bestDesc, bestOK)
	} else {
		fmt.Fprintln(w, "➜ B 轨在有效范围内所有点位核心窗口均无通透小时；"+
			"若多为「无数据」请看下方归因，多数情况改用 --source openmeteo 即可拿到结论。")
	}

	fmt.Fprintln(w)
	for _, l := range TomorrowNoDataSummaryLines(tracks) {
		fmt.Fprintln(w, l)
	}
	fmt.Fprintln(w)
	for _, l := range TomorrowCapabilityLines(tracks) {
		fmt.Fprintln(w, l)
	}
}

var TomorrowReasonOrder = []dualtrack.NoDataReason{
	dualtrack.RoundQuotaDown,
	dualtrack.OutOfHorizon,
	dualtrack.KeyMissing,
	dualtrack.SemanticFailure,
	dualtrack.AmbiguousBase,
}

func TomorrowReasonCounts(tracks []*dualtrack.TrackResult) []TomorrowReasonCount {
	out := make([]TomorrowReasonCount, 0, len(TomorrowReasonOrder))
	for _, reason := range TomorrowReasonOrder {
		n := 0
		for _, tr := range tracks {
			if tr == nil {
				continue
			}
			n += tr.CountByReason(reason)
		}
		if n > 0 {
			out = append(out, TomorrowReasonCount{
				Reason: reason,
				Label:  reason.Label(),
				Count:  n,
			})
		}
	}
	return out
}

type TomorrowReasonCount struct {
	Reason dualtrack.NoDataReason
	Label  string
	Count  int
}

func TomorrowRowTotals(tracks []*dualtrack.TrackResult) (total, nodata int) {
	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		total += len(tr.Rows)
		nodata += tr.NoDataCount()
	}
	return total, nodata
}

func TomorrowNoDataSummaryLines(tracks []*dualtrack.TrackResult) []string {
	total, nodata := TomorrowRowTotals(tracks)
	if total == 0 {
		return []string{"【无数据归因】B 轨本轮没有产出任何时次。"}
	}
	if nodata == 0 {
		return []string{fmt.Sprintf("【无数据归因】%d 个时次全部可判，无 %s。",
			total, model.RATING_NODATA)}
	}

	parts := make([]string, 0, len(TomorrowReasonOrder))
	for _, rc := range TomorrowReasonCounts(tracks) {
		parts = append(parts, fmt.Sprintf("[%s] %d", rc.Label, rc.Count))
	}
	lines := []string{
		fmt.Sprintf("【无数据归因】%s %d/%d 个时次；分项：%s",
			model.RATING_NODATA, nodata, total, strings.Join(parts, "  ")),
	}

	if next := TomorrowNextAvailable(tracks); next != nil {
		lines = append(lines, fmt.Sprintf("             配额预计恢复：%s（届时重跑即可）",
			next.Format(tomorrowStampLayout)))
	}
	return lines
}

func TomorrowNextAvailable(tracks []*dualtrack.TrackResult) *time.Time {
	var best *time.Time
	for _, tr := range tracks {
		if tr == nil || tr.NextAvailable == nil {
			continue
		}
		t := *tr.NextAvailable
		if t.IsZero() {
			continue
		}
		if best == nil || t.Before(*best) {
			v := t
			best = &v
		}
	}
	return best
}

func TomorrowQuotaExhausted(tracks []*dualtrack.TrackResult) bool {
	for _, tr := range tracks {
		if tr != nil && tr.QuotaExhausted {
			return true
		}
	}
	return false
}

func TomorrowCapabilityLines(tracks []*dualtrack.TrackResult) []string {
	lines := []string{
		"【本轨能力边界】",
		"  · 无云顶字段：云底低于机位时，「脚下云海」与「机位在云中」不可区分，一律判 " +
			model.RATING_NODATA + " [" + dualtrack.AmbiguousBase.Label() + "]",
		"  · 云海判定请改用 --source openmeteo（A 轨有气压层廓线，可反演云顶）",
		"  · ΔH 只用于标注这一列可信到什么程度，不参与评级计算",
	}

	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		if len(tr.ThresholdsReused) > 0 {
			lines = append(lines, "  · 复用 A 轨阈值："+strings.Join(tr.ThresholdsReused, "、"))
		}
		if len(tr.ThresholdsUnavailable) > 0 {
			lines = append(lines, "  · 本轨结构性用不上的阈值（缺输入，非未实现）："+
				strings.Join(tr.ThresholdsUnavailable, "、"))
		}
		break
	}
	return lines
}
