package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
)

type TomorrowSiteNightStats struct {
	SiteID string
	Night  string

	Planned   int
	Judgeable int
	NoData    int

	OK   int
	Warn int
	Bad  int

	Reasons []TomorrowReasonCount

	BestWindow string

	AboveMin model.OptFloat
	AboveMax model.OptFloat

	Fidelity dualtrack.TerrainFidelity

	QuotaExhausted bool
	Verdict        string
}

func tomorrowCoreRows(tr *dualtrack.TrackResult, night string,
	cfg config.Config) []dualtrack.HourVerdict {

	if tr == nil {
		return nil
	}
	out := make([]dualtrack.HourVerdict, 0, 8)
	for _, v := range tr.Rows {
		if night != "" && TomorrowNightID(v.TimeLocal) != night {
			continue
		}
		if !InCoreWindow(v.TimeLocal.Hour(), cfg.Window) {
			continue
		}
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return tomorrowLocalISO(out[i].TimeLocal) < tomorrowLocalISO(out[j].TimeLocal)
	})
	return out
}

func ComputeTomorrowSiteNightStats(tr *dualtrack.TrackResult, night string,
	cfg config.Config) TomorrowSiteNightStats {

	st := TomorrowSiteNightStats{Night: night, BestWindow: "无", Fidelity: dualtrack.TerrainUnknown}
	if tr == nil {
		st.Verdict = "❓ 该点位无 B 轨结果"
		return st
	}
	st.SiteID = tr.SiteID
	st.QuotaExhausted = tr.QuotaExhausted

	core := tomorrowCoreRows(tr, night, cfg)
	st.Planned = len(core)
	if st.Planned == 0 {
		st.Verdict = "❓ 核心窗口内无任何时次"
		return st
	}

	reasonCounts := make(map[dualtrack.NoDataReason]int, len(TomorrowReasonOrder))
	fidelityCounts := make(map[dualtrack.TerrainFidelity]int, 4)
	fidelityOrder := make([]dualtrack.TerrainFidelity, 0, 4)
	aboves := make([]float64, 0, len(core))

	for _, v := range core {
		if v.IsNoData() {
			st.NoData++
			reasonCounts[v.NoDataReason]++
		} else {
			st.Judgeable++
			if v.CloudBaseAboveSite.Valid {
				aboves = append(aboves, v.CloudBaseAboveSite.V)
			}
		}
		switch v.Rating {
		case model.RATING_OK:
			st.OK++
		case model.RATING_WARN:
			st.Warn++
		case model.RATING_BAD:
			st.Bad++
		}
		if _, seen := fidelityCounts[v.TerrainFidelity]; !seen {
			fidelityOrder = append(fidelityOrder, v.TerrainFidelity)
		}
		fidelityCounts[v.TerrainFidelity]++
	}

	for _, reason := range TomorrowReasonOrder {
		if n := reasonCounts[reason]; n > 0 {
			st.Reasons = append(st.Reasons, TomorrowReasonCount{
				Reason: reason, Label: reason.Label(), Count: n,
			})
		}
	}

	bestN := 0
	for _, f := range fidelityOrder {
		if fidelityCounts[f] > bestN {
			bestN = fidelityCounts[f]
			st.Fidelity = f
		}
	}

	st.AboveMin, st.AboveMax = minMax(aboves)
	if st.OK > 0 {
		st.BestWindow = TomorrowLongestRun(core, model.RATING_OK)
	}
	st.Verdict = tomorrowVerdict(st)
	return st
}

func tomorrowVerdict(st TomorrowSiteNightStats) string {
	switch {
	case st.QuotaExhausted:
		return "❓ 配额耗尽，本轮未取数；等配额恢复后重跑"
	case st.Judgeable == 0 && st.NoData > 0:

		for _, rc := range st.Reasons {
			if rc.Reason == dualtrack.AmbiguousBase {
				return "❓ 全夜云底低于机位不可判 —— 这是 B 轨能力边界，请改用 --source openmeteo"
			}
		}
		if len(st.Reasons) > 0 {
			return "❓ 全夜无可判时次（" + st.Reasons[0].Label + "）"
		}
		return "❓ 全夜无可判时次"
	case st.Judgeable == 0:
		return "❓ 核心窗口内无有效时次，请临近再跑"
	case st.OK >= 3:
		return "✅ 可去"
	case st.OK > 0:
		return "⚠️ 窗口偏短，需现场判断"
	case st.Warn > 0:
		return "⚠️ 有机会但不稳定"
	default:
		return "🔴 建议放弃该点位"
	}
}

func (st TomorrowSiteNightStats) Line() string {
	head := fmt.Sprintf("%s 可判 %d/%d", Pad(st.SiteID, 10, AlignLeft), st.Judgeable, st.Planned)
	if st.NoData > 0 {
		tags := make([]string, 0, len(st.Reasons))
		for _, rc := range st.Reasons {
			tags = append(tags, fmt.Sprintf("[%s]%d", rc.Label, rc.Count))
		}
		head += fmt.Sprintf("（无数据 %d：%s）", st.NoData, strings.Join(tags, " "))
	}
	head += fmt.Sprintf("：通透 %dh / 风险 %dh / 不宜 %dh", st.OK, st.Warn, st.Bad)
	if st.OK > 0 {
		head += "；最佳连续窗口 " + st.BestWindow
	}
	return head + "；" + st.Verdict
}

func TomorrowLongestRun(rows []dualtrack.HourVerdict, rating string) string {
	var best, cur []dualtrack.HourVerdict
	for _, v := range rows {
		if v.Rating == rating {
			cur = append(cur, v)
			if len(cur) > len(best) {
				best = append([]dualtrack.HourVerdict(nil), cur...)
			}
		} else {
			cur = nil
		}
	}
	if len(best) == 0 {
		return "无"
	}
	first := best[0].TimeLocal
	last := best[len(best)-1].TimeLocal.Add(time.Hour)
	if first.IsZero() || last.IsZero() {
		return "无"
	}
	return fmt.Sprintf("%s-%s（%dh）", first.Format("15:04"), last.Format("15:04"), len(best))
}

func mdTomorrowNightlySection(tracks []*dualtrack.TrackResult, meta model.ReportMeta,
	cfg config.Config) []string {

	nights := TomorrowNightKeys(tracks)
	lines := []string{"## 二、各观测夜汇总（" + TomorrowTrackLabel + "）", ""}

	if len(nights) == 0 || len(tracks) == 0 {
		return append(lines, "本次运行 B 轨未产出任何时次，无法给出汇总。", "")
	}

	if len(meta.Sites) > 0 {
		lines = append(lines, "### 2.1 天文条件（近似算法，取各点位经纬度均值；与数据源无关）", "")
		astroRows := make([][]string, 0, len(nights))
		for _, night := range nights {
			ast := ComputeNightAstro(night, meta.Sites, cfg, int(meta.UTCOffsetHours*3600))
			moonUp := "整夜在地平线下"
			if ast.MoonUpHours > 0 {
				moonUp = strconv.Itoa(ast.MoonUpHours) + "h"
			}
			astroRows = append(astroRows, []string{
				night + " 夜",
				fmt.Sprintf("%s %s%%", ast.MoonPhase, FormatFixed(ast.MoonIllumPct, 0)),
				moonUp,
				strconv.Itoa(ast.DarkHours) + "h",
				FormatFixed(ast.GCMax, 0) + "°",
			})
		}
		lines = append(lines, MDTable(
			[]string{"观测夜", "月相/照度", "月亮在地平线上", "天文暗夜时长", "银心最高高度"},
			astroRows)...)
		lines = append(lines, "")
	}

	lines = append(lines,
		fmt.Sprintf("### 2.2 核心窗口 %02d:00–%02d:00 通透小时数",
			cfg.Window.CoreStartHour, cfg.Window.CoreEndHour),
		"",
		"单元格格式为 `通透h / 可判h`。**分母是「可判小时数」而不是「有数据小时数」**："+
			"B 轨的无数据里既有真缺测，也有「数据齐全但云底低于机位、结论不可判」，"+
			"两者混在一个分母里会让通透率失真。`无数据` 表示该夜该点位一个可判时次都没有。",
		"",
	)

	bestOK, bestDesc := -1, ""
	matrixRows := make([][]string, 0, len(tracks))
	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		cells := make([]string, 0, len(nights)+1)
		cells = append(cells, tr.SiteID)
		for _, night := range nights {
			st := ComputeTomorrowSiteNightStats(tr, night, cfg)
			if st.Judgeable == 0 {
				cells = append(cells, "无数据")
				continue
			}
			cells = append(cells, fmt.Sprintf("%d / %d", st.OK, st.Judgeable))
			if st.OK > bestOK {
				bestOK = st.OK
				bestDesc = fmt.Sprintf("%s @ %s 夜", tr.SiteID, night)
			}
		}
		matrixRows = append(matrixRows, cells)
	}
	headers := make([]string, 0, len(nights)+1)
	headers = append(headers, "点位")
	for _, n := range nights {
		headers = append(headers, n+" 夜")
	}
	lines = append(lines, MDTable(headers, matrixRows)...)

	lines = append(lines, "")
	if bestOK > 0 {
		lines = append(lines,
			fmt.Sprintf("**综合最优机位**：%s（核心窗口 %dh 通透）", bestDesc, bestOK))
	} else {
		lines = append(lines, "**综合最优机位**：B 轨在有效范围内所有点位核心窗口均无通透小时。"+
			"请看下一节的无数据归因；若归因多为「"+dualtrack.AmbiguousBase.Label()+
			"」，说明这批点位是被模式抹平的孤立高峰，应改用 `--source openmeteo`。")
	}
	lines = append(lines, "")
	return lines
}

func mdTomorrowReasonSection(tracks []*dualtrack.TrackResult) []string {
	total, nodata := TomorrowRowTotals(tracks)
	lines := []string{
		"## 三、无数据归因与本轨能力边界",
		"",
	}

	if total == 0 {
		return append(lines, "本次运行 B 轨没有产出任何时次。", "")
	}

	lines = append(lines, fmt.Sprintf("本轮共 %d 个时次，其中 %s %d 个（%s）。",
		total, model.RATING_NODATA, nodata, tomorrowPct(nodata, total)), "")

	if nodata > 0 {
		reasonRows := make([][]string, 0, len(TomorrowReasonOrder))
		for _, rc := range TomorrowReasonCounts(tracks) {
			reasonRows = append(reasonRows, []string{
				"[" + rc.Label + "]",
				string(rc.Reason),
				strconv.Itoa(rc.Count),
				tomorrowPct(rc.Count, total),
				tomorrowReasonAdvice(rc.Reason),
			})
		}
		lines = append(lines, MDTable(
			[]string{"归因标签", "枚举值", "时次数", "占比", "该怎么办"}, reasonRows)...)
		lines = append(lines, "")
	}

	if TomorrowQuotaExhausted(tracks) {
		msg := "**配额耗尽**：本轮有点位因 Tomorrow.io 免费配额耗尽而未发起任何请求，" +
			"这些时次一律记为 `" + model.RATING_NODATA + " [" + dualtrack.RoundQuotaDown.Label() + "]`。"
		if next := TomorrowNextAvailable(tracks); next != nil {
			msg += fmt.Sprintf("配额预计于 **%s** 恢复，届时重跑即可。",
				next.Format(tomorrowStampLayout))
		}
		lines = append(lines, msg, "")
	}

	lines = append(lines, "### 3.1 本轨能力边界（恒定属性，与本轮数据无关）", "")
	lines = append(lines,
		"- **没有云顶字段**：Tomorrow.io 只提供 `cloudBase` / `cloudCeiling`，没有云顶高度。"+
			"因此云底低于机位时，「脚下云海」与「机位在云中」在数学上不可区分，"+
			"一律判 `"+model.RATING_NODATA+" ["+dualtrack.AmbiguousBase.Label()+"]`。",
		"- **云海判定一律以 A 轨为准**：需要判脚下云海请改用 `--source openmeteo`"+
			"（A 轨有多层气压面湿度廓线，可反演云顶）。",
		"- **云底基准是模式地形**：接口给的 `cloudBase` 相对的是模式网格地形，不是真实机位。"+
			"本报告已用反解出的模式地形高度订正到机位（`云底相对机位 = H_model + 云底AGL(模式) − 机位海拔`）。",
		"- **ΔH 是诊断量，不进评级**：`ΔH = H_model − 机位海拔`，衡量模式把这个山头削平了多少。"+
			"它只驱动「保真度」标注，评级链只消费「云底相对机位」。",
		"",
	)

	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		if len(tr.ThresholdsReused) > 0 {
			lines = append(lines, "- **复用 A 轨阈值**：`"+
				strings.Join(tr.ThresholdsReused, "`、`")+"`（两轨同源，不是各调各的）。")
		}
		if len(tr.ThresholdsUnavailable) > 0 {
			lines = append(lines, "- **本轨结构性用不上的阈值**：`"+
				strings.Join(tr.ThresholdsUnavailable, "`、`")+
				"`。「结构性」指缺少输入本身（垂直廓线、云顶、露点），不是「这版没实现」。")
		}
		break
	}
	lines = append(lines, "")
	return lines
}

func tomorrowReasonAdvice(r dualtrack.NoDataReason) string {
	switch r {
	case dualtrack.RoundQuotaDown:
		return "等免费配额窗口重置后重跑；或改用 --source openmeteo（无配额限制）"
	case dualtrack.OutOfHorizon:
		return "超出 Tomorrow.io 的 120h/5 天预报窗，临近观测日再跑"
	case dualtrack.KeyMissing:
		return "上游关键字段缺测，可稍后重试；持续出现请改用 --source openmeteo"
	case dualtrack.SemanticFailure:
		return "上游数据自相矛盾或违反本轨契约，属可修缺陷，请向维护者反馈"
	case dualtrack.AmbiguousBase:
		return "B 轨能力边界（无云顶字段），无解；改用 --source openmeteo"
	default:
		return "—"
	}
}

func tomorrowPct(n, total int) string {
	if total <= 0 {
		return MissingCell
	}
	return FormatFixed(float64(n)*100.0/float64(total), 1) + "%"
}

func mdTomorrowDetailSection(tracks []*dualtrack.TrackResult, cfg config.Config) []string {
	lines := []string{
		"## 四、逐时判定明细（" + TomorrowTrackLabel + "）",
		"",
		fmt.Sprintf("统计口径为核心窗口 %02d:00–%02d:00。",
			cfg.Window.CoreStartHour, cfg.Window.CoreEndHour),
		"",
		"列义：`云底相对机位` 已订正到真实机位（正=云在头顶、≤0=落入不可判歧义桶）；" +
			"`云底AGL(模式)` 是接口原值（相对模式地形）；`ΔH` = 模式地形高度 − 机位海拔。",
		"",
	}
	if len(tracks) == 0 {
		return append(lines, "本次运行 B 轨无可用明细。", "")
	}

	nights := TomorrowNightKeys(tracks)
	headers := []string{"点位", "可判h", "通透h", "风险h", "不宜h", "无数据h",
		"最佳连续通透窗口", "云底相对机位范围m", "地形保真度", "结论"}

	for _, night := range nights {
		lines = append(lines,
			fmt.Sprintf("### %s 夜（%s 22:00 → 次日 06:00）", night, night), "")

		nightRows := make([][]string, 0, len(tracks))
		for _, tr := range tracks {
			if tr == nil {
				continue
			}
			st := ComputeTomorrowSiteNightStats(tr, night, cfg)
			nightRows = append(nightRows, []string{
				tr.SiteID,
				fmt.Sprintf("%d/%d", st.Judgeable, st.Planned),
				strconv.Itoa(st.OK),
				strconv.Itoa(st.Warn),
				strconv.Itoa(st.Bad),
				strconv.Itoa(st.NoData),
				st.BestWindow,
				fmtRange(st.AboveMin, st.AboveMax, st.Judgeable > 0),
				st.Fidelity.Label(),
				st.Verdict,
			})
		}
		lines = append(lines, MDTable(headers, nightRows)...)
		lines = append(lines, "")
	}

	lines = append(lines,
		"> 上表为每夜每点位的**精简汇总**。完整的逐小时明细（含每个时次的云底、ΔH、"+
			"保真度、无数据归因）请用 `--csv` 或 `--json` 导出后查看。",
		"",
	)
	return lines
}

func mdTomorrowLegendSection() []string {
	lines := []string{
		"## 五、导出字段说明（" + TomorrowTrackLabel + " CSV / JSON 字段对照）",
		"",
		"> CSV 表头已为中文；JSON 顶层带 `field_labels` 字段。下表供二次分析时对照英文 key。",
		"> 注意本轨字段与 A 轨（Open-Meteo）**不通用**：没有云顶/云厚，多出 ΔH 与保真度。",
		"",
	}
	legendRows := make([][]string, 0, len(TomorrowCSVFields))
	for _, k := range TomorrowCSVFields {
		label, ok := TomorrowFieldLabels[k]
		if !ok {
			label = k
		}
		legendRows = append(legendRows, []string{k, label, TomorrowFieldNotes[k]})
	}
	lines = append(lines, MDTable([]string{"字段名", "中文含义", "说明"}, legendRows)...)
	lines = append(lines, "")
	return lines
}

func mdTomorrowNoDataBanner(tracks []*dualtrack.TrackResult) []string {
	head := "> ## ⛔ 本次运行 B 轨没有取得任何可判时次"
	reason := "> 全部时次判为 `" + model.RATING_NODATA + "`。**这不一定是故障**，" +
		"请先看下文「无数据归因」一节确认属于哪一类。"
	if TomorrowQuotaExhausted(tracks) {
		reason = "> 本轮 Tomorrow.io **免费配额已耗尽**，未发起任何请求，" +
			"因此全部时次判为 `" + model.RATING_NODATA + " [" +
			dualtrack.RoundQuotaDown.Label() + "]`。"
		if next := TomorrowNextAvailable(tracks); next != nil {
			reason += fmt.Sprintf("配额预计于 **%s** 恢复。",
				next.Format(tomorrowStampLayout))
		}
	}
	return []string{
		head,
		">",
		reason,
		">",
		"> 若急需结论，改用 `--source openmeteo`（A 轨无配额限制，且能判脚下云海）。" +
			"本报告仍保留元信息与「拍摄影响因素权重说明」，可先用于选点与器材准备。",
		"",
	}
}

func BuildTomorrowMarkdownReport(tracks []*dualtrack.TrackResult,
	meta model.ReportMeta, cfg config.Config) string {

	lines := make([]string, 0, 256)
	lines = append(lines, mdHeadSection(meta, cfg)...)

	judgeable := 0
	for _, tr := range tracks {
		if tr == nil {
			continue
		}
		for _, v := range tr.Rows {
			if !v.IsNoData() {
				judgeable++
			}
		}
	}
	if judgeable == 0 {
		lines = append(lines, mdTomorrowNoDataBanner(tracks)...)
	}

	lines = append(lines, mdTomorrowNightlySection(tracks, meta, cfg)...)
	lines = append(lines, mdTomorrowReasonSection(tracks)...)
	lines = append(lines, mdTomorrowDetailSection(tracks, cfg)...)
	lines = append(lines, mdTomorrowLegendSection()...)

	source := meta.Source
	if source == "" {
		source = MetaSourceTomorrow
	}
	lines = append(lines,
		"---",
		"",
		fmt.Sprintf("*由 `astro-mountain` 自动生成 · 数据源 %s · 单轨制：本报告只呈现所选那一轨的结论。%s*",
			source, meta.Disclaimer),
		"",
	)
	return strings.Join(lines, "\n")
}

func WriteTomorrowMarkdownReport(tracks []*dualtrack.TrackResult, meta model.ReportMeta,
	cfg config.Config, outDir string) (string, error) {

	if outDir == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("创建报告目录 %s 失败：%w", outDir, err)
	}
	path := filepath.Join(outDir, ReportFilename(meta))
	text := BuildTomorrowMarkdownReport(tracks, meta, cfg)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", fmt.Errorf("写入报告 %s 失败：%w", path, err)
	}
	return path, nil
}
