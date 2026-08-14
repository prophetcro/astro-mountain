package report

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func mdHeadSection(meta model.ReportMeta, cfg config.Config) []string {
	generated := meta.GeneratedAt
	if generated == "" {
		generated = "-"
	}
	w, t := cfg.Window, cfg.Thresh

	lines := []string{
		"# 山地星空 / 流星雨 低云海拔评估报告",
		"",
		fmt.Sprintf("> **生成时间**：%s（本地时间）  ", generated),
	}
	lines = append(lines, mdSourceLines(meta)...)
	lines = append(lines,
		"",
		"## 一、元信息",
		"",
	)

	info := [][]string{{"数值模式", meta.Models}}
	if meta.Peak.Valid && meta.Peak.V != "" {
		peakDesc := meta.Peak.V
		if meta.Days != nil {
			peakDesc += fmt.Sprintf("（覆盖极大前 %d 天至极大夜）", *meta.Days)
		}
		info = append(info, []string{"流星雨极大日", peakDesc})
	}

	timezone := meta.Timezone
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	info = append(info,
		[]string{"查询日期范围", meta.Start + " ~ " + meta.End},
		[]string{"观测夜", orDash(meta.NightsDesc)},
		[]string{"时区", fmt.Sprintf("%s (UTC+%s)", timezone, FormatG(meta.UTCOffsetHours))},
		[]string{"夜间窗口", fmt.Sprintf("%02d:00 ~ %02d:00（北京时间，跨零点）",
			w.NightStartHour, w.NightEndHour)},
		[]string{"核心窗口", fmt.Sprintf("%02d:00 ~ %02d:00（本报告所有统计口径）",
			w.CoreStartHour, w.CoreEndHour)},
		[]string{"云层判据", fmt.Sprintf("层云量 ≥ %s%% 或 RH ≥ %s%%(低层)/%s%%(高层)",
			FormatFixed(t.CloudCoverThreshold, 0), FormatFixed(t.RHThresholdLow, 0),
			FormatFixed(t.RHThresholdHigh, 0))},
		[]string{"雾判据", fmt.Sprintf("能见度 < %sm；模式无 visibility 时退化为近地 RH ≥ %s%% 代理",
			FormatFixed(t.FogVisibilityM, 0), FormatFixed(t.FogProxyRHHigh, 0))},
		[]string{"时次统计", fmt.Sprintf("夜间窗口共 %d 时次，其中 %d 时次有有效预报",
			meta.HoursTotal, meta.HoursWithData)},
		[]string{"能见度可用", boolCN(meta.VisibilityAvailable, "是", "否（已退化为近地 RH 代理判据）")},
		[]string{"生成时间", generated},
	)
	lines = append(lines, MDTable([]string{"项目", "内容"}, info)...)

	lines = append(lines, "", "### 1.1 点位列表", "")
	siteRows := make([][]string, 0, len(meta.Sites))
	for _, s := range meta.Sites {
		siteRows = append(siteRows, []string{
			s.Name,
			FormatFixed(s.Lat, 4),
			FormatFixed(s.Lon, 4),
			FormatG(s.Alt),
		})
	}
	lines = append(lines, MDTable([]string{"点位", "纬度", "经度", "海拔(m)"}, siteRows)...)
	lines = append(lines, "")
	return lines
}

func mdSourceLines(meta model.ReportMeta) []string {
	if IsTomorrowSource(meta.Source) {
		return []string{
			"> **数据源**：" + TomorrowTrackLabel + " · 云底高度直接产品（cloudBase，相对模式地形）· " +
				"已用反解出的模式地形高度订正到真实机位  ",
			"> **说明**：本轨没有云顶字段 —— 云底低于机位时「脚下云海」与「机位在云中」" +
				"不可区分，一律判无数据；云海判定请改用 `--source openmeteo`。" +
				"天文量为纯 Go 近似算法结果，均非观测实测值。出发前请再跑一次以取最新预报。",
		}
	}
	if meta.Source == MetaSourceMeteoblue {
		return []string{
			"> **数据源**：Meteoblue 融合预报 · 分层云量 / 降水 / 能见度（Basic-1h + Clouds-1h）  ",
			"> **说明**：Meteoblue **不反演云海几何**（没有气压层，给不出云底/云顶），" +
				"只判通透度、降水与能见度，因此本报告**不含**「云海在脚下 / 机位在云中」等几何结论；" +
				"云海判定请改用 `--source openmeteo`。" +
				"天文量为纯 Go 近似算法结果，均非观测实测值。出发前请再跑一次以取最新预报。",
		}
	}
	return []string{
		fmt.Sprintf("> **数据源**：Open-Meteo 免费 API · 模式 `%s` · "+
			"气压层剖面反演（非 LCL 估算）  ", meta.Models),

		"> **说明**：云底/云顶为数值模式反演值，天文量为纯 Go 近似算法结果，" +
			"均非观测实测值。出发前请再跑一次以取最新预报。",
	}
}

func mdNightlySection(rows []model.HourRow, compare []model.ModelCompareRow,
	meta model.ReportMeta, cfg config.Config) []string {

	nights, sites := meta.Nights, meta.Sites
	lines := []string{"## 二、各观测夜汇总", ""}

	if len(nights) == 0 || len(sites) == 0 {
		return append(lines, "本次运行未解析出任何观测夜或点位，无法给出汇总。", "")
	}

	if len(compare) > 0 {
		lines = append(lines,
			"> 本页「通透小时数」已按 **ICON + GFS 两模型共识** 统计（只计两模型都判通透的时次）。",
			"",
		)
	}

	lines = append(lines, "### 2.1 天文条件（近似算法，取各点位经纬度均值）", "")
	astroRows := make([][]string, 0, len(nights))
	for _, night := range nights {
		st := ComputeNightAstro(night, sites, cfg, int(meta.UTCOffsetHours*3600))
		moonUp := "整夜在地平线下"
		if st.MoonUpHours > 0 {
			moonUp = strconv.Itoa(st.MoonUpHours) + "h"
		}
		astroRows = append(astroRows, []string{
			night + " 夜",
			fmt.Sprintf("%s %s%%", st.MoonPhase, FormatFixed(st.MoonIllumPct, 0)),
			moonUp,
			strconv.Itoa(st.DarkHours) + "h",
			FormatFixed(st.GCMax, 0) + "°",
		})
	}
	lines = append(lines, MDTable(
		[]string{"观测夜", "月相/照度", "月亮在地平线上", "天文暗夜时长", "银心最高高度"},
		astroRows)...)

	lines = append(lines,
		"",
		fmt.Sprintf("### 2.2 核心窗口 %02d:00–%02d:00 通透小时数",
			cfg.Window.CoreStartHour, cfg.Window.CoreEndHour),
		"",
		"单元格格式为 `通透h / 有效h`；`无数据` 表示该夜该点位全部时次超出模式预报时效。",
		"",
	)

	bestOK, bestDesc := -1, ""
	hasCompare := len(compare) > 0
	matrixRows := make([][]string, 0, len(sites))
	for _, site := range sites {
		cells := make([]string, 0, len(nights)+1)
		cells = append(cells, site.Name)
		for _, night := range nights {
			sw, _ := SunriseWindowForNight(site, night, int(meta.UTCOffsetHours*3600), cfg)
			st := ComputeSiteNightStats(site.Name, night, rows, compare, cfg, sw)
			if st.Valid == 0 {
				cells = append(cells, "无数据")
				continue
			}
			ok := st.OK
			if hasCompare {
				ok = st.CrossOK
			}
			cells = append(cells, fmt.Sprintf("%d / %d", ok, st.Valid))
			if ok > bestOK {
				bestOK = ok
				bestDesc = fmt.Sprintf("%s @ %s 夜", site.Name, night)
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
		lines = append(lines, "**综合最优机位**：有效预报范围内所有点位核心窗口均无通透小时。"+
			"若上表多为「无数据」，请在极大日前 5~7 天内重跑；"+
			"否则建议顺延日期或更换区域。")
	}
	lines = append(lines, "")
	return lines
}

func mdWeightSection(cfg config.Config) []string {
	factors := ImpactFactors(cfg)
	lines := []string{
		"## 三、拍摄影响因素权重说明",
		"",
		"> **以下权重为经验性评估，非严格模型**：它来自山地星野/流星摄影的实拍经验，" +
			"用于在多个条件冲突时排定取舍优先级，不能当作可回归验证的定量指标。" +
			"不同焦段、不同题材（纯流星 / 流星+银河 / 星野风光）应自行调整。  ",
		fmt.Sprintf("> **诚实性声明**：标注「%s」的量由本脚本直接算出并已体现在上文表格中；"+
			"标注「%s」的量本脚本**并不计算**，需要你自行查证后人工加权。",
			SrcAuto, SrcManual),
		"",
	}

	factorRows := make([][]string, 0, len(factors))
	for _, f := range factors {
		factorRows = append(factorRows, []string{
			f.Name, f.Effect, f.Level, strconv.Itoa(f.Weight) + "%", f.Source, f.Reason,
		})
	}
	lines = append(lines, MDTable(
		[]string{"因素", "对拍摄的影响", "影响程度", "建议权重", "数据来源/说明", "权重理由"},
		factorRows)...)

	var auto, manual []string
	var autoW, manualW int
	for _, f := range factors {
		if f.IsAuto() {
			auto = append(auto, f.Name)
			autoW += f.Weight
			continue
		}
		manual = append(manual, f.Name)
		manualW += f.Weight
	}

	lines = append(lines,
		"",
		fmt.Sprintf("**权重合计**：%d%%", ImpactWeightTotal(factors)),
		"",
		fmt.Sprintf("- 本脚本自动计算（%d 项，合计 %d%%）：%s",
			len(auto), autoW, strings.Join(auto, "、")),
		fmt.Sprintf("- 需人工/外部数据（%d 项，合计 %d%%）：%s",
			len(manual), manualW, strings.Join(manual, "、")),
		"",
	)
	return lines
}

func annotateRelation(relLabel, mainReason string) string {
	if relLabel != "云海在脚下" {
		return relLabel
	}
	if strings.Contains(mainReason, "浓雾") || strings.Contains(mainReason, "降水") {
		return "云海在脚下（浓雾/降水遮蔽，不可见）"
	}
	return relLabel
}

func mdDetailSection(rows []model.HourRow, compare []model.ModelCompareRow,
	meta model.ReportMeta, cfg config.Config) []string {

	nights, sites := meta.Nights, meta.Sites
	lines := []string{
		"## 四、低云海拔评估明细",
		"",
		fmt.Sprintf("统计口径为核心窗口 %02d:00–%02d:00，取每夜决定评级的那层云。",
			cfg.Window.CoreStartHour, cfg.Window.CoreEndHour),
		"",
		"`云底/云顶AGL` 均为**相对机位**高度（正=在机位之上，负=在机位之下），" +
			"两者符号组合即对应「主要状态」：",
		"",
		"- 云底 `+`、云顶 `+` → 云在头顶（被遮挡）",
		"- 云底 `-`、云顶 `+` → 机位在云中（不可拍摄）",
		"- 云底 `-`、云顶 `-` → 云海在脚下（头顶通透，最佳；若被浓雾/降水封住会在该格括注\"不可见\"）",
		"",
	}
	if len(nights) == 0 || len(sites) == 0 {
		return append(lines, "本次运行无可用明细。", "")
	}

	headers := []string{"点位", "海拔m", "有效h", "通透h", "风险h", "不宜h",
		"最佳连续通透窗口", "云底AGL范围m", "云顶AGL范围m", "主要状态", "日出窗云海", "主要诱因", "结论"}

	for _, night := range nights {
		lines = append(lines,
			fmt.Sprintf("### %s 夜（%s 22:00 → 次日 06:00）", night, night), "")
		lines = append(lines, nightAstroLine(night, sites, cfg, int(meta.UTCOffsetHours*3600)), "")

		nightRows := make([][]string, 0, len(sites))
		for _, site := range sites {
			sw, _ := SunriseWindowForNight(site, night, int(meta.UTCOffsetHours*3600), cfg)
			st := ComputeSiteNightStats(site.Name, night, rows, compare, cfg, sw)
			relLabel := MissingCell
			if st.DominantRelation != "" {
				if label, ok := model.RelLabels[st.DominantRelation]; ok {
					relLabel = label
				}
			}
			okStr := strconv.Itoa(st.OK)
			if len(compare) > 0 {
				okStr = fmt.Sprintf("%d(%d)", st.CrossOK, st.OK)
			}
			nightRows = append(nightRows, []string{
				site.Name,
				FormatG(site.Alt),
				fmt.Sprintf("%d/%d", st.Valid, st.Planned),
				okStr,
				strconv.Itoa(st.Warn),
				strconv.Itoa(st.Bad),
				st.BestWindow,
				fmtRange(st.BaseAGLMin, st.BaseAGLMax, st.Valid > 0),
				fmtRange(st.TopAGLMin, st.TopAGLMax, st.Valid > 0),
				annotateRelation(relLabel, st.MainReason),
				st.CloudSea,
				st.MainReason,
				st.Verdict,
			})
		}
		lines = append(lines, MDTable(headers, nightRows)...)
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf(
			"> 「日出窗云海」仅统计日出前后各 %d/%d 分钟窗口内的云海状况（见配置 `sunrise_window_before_min`/`after_min`），"+
				"反映日出拍摄时分的机位下方云海，而非整夜。窗口内无采样时次则记「无」。",
			cfg.Window.SunriseWindowBeforeMin, cfg.Window.SunriseWindowAfterMin))
		lines = append(lines, "")
	}

	lines = append(lines,
		"> 上表为每夜每点位的**精简汇总**。完整的逐小时明细（含各气压层反演出的每一层云的"+
			"云底/云顶/云量/云厚、能见度、温露差、月亮与银心高度等）请用 `--csv` 或 "+
			"`--json` 导出后查看。",
		"",
	)
	return lines
}

func mdFieldLegendSection() []string {
	lines := []string{
		"## 五、导出字段说明（CSV / JSON 字段对照）",
		"",
		"> CSV 表头已为中文；JSON 顶层带 `field_labels` 字段。下表供二次分析时对照英文 key。",
		"",
	}
	legendRows := make([][]string, 0, len(CSVFields))
	for _, k := range CSVFields {
		label, ok := FieldLabels[k]
		if !ok {
			label = k
		}
		legendRows = append(legendRows, []string{k, label, FieldNotes[k]})
	}
	lines = append(lines, MDTable([]string{"字段名", "中文含义", "说明"}, legendRows)...)
	lines = append(lines, "")
	return lines
}

func mdNoForecastBanner() []string {
	return []string{
		"> ## ⛔ 本次运行没有取得任何有效预报",
		">",
		"> 所选观测夜**没有任何有效预报数据**：数值模式未返回气压层云量等必要字段，" +
			"或确实超出该模式预报时效，或该时段全部时次缺测，因此下文所有气象统计均为空。",
		">",
		"> **请改用 icon_seamless 或 gfs_seamless**，或在流星雨极大日前 5~7 天再跑一次**本脚本**，届时预报才会覆盖目标观测夜。" +
			"本报告仍保留元信息与「拍摄影响因素权重说明」，可先用于选点与器材准备。",
		"",
	}
}

func orDash(s string) string {
	if s == "" {
		return MissingCell
	}
	return s
}

func boolCN(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}
