package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/astro"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func testMeta() model.ReportMeta {
	days := 2
	return model.ReportMeta{
		Models:     "icon_seamless",
		Start:      "2026-08-11",
		End:        "2026-08-14",
		Nights:     []string{"2026-08-11", "2026-08-12", "2026-08-13"},
		NightsDesc: "2026-08-11 ~ 2026-08-13 共 3 夜（每夜 22:00 → 次日 06:00；极大日 2026-08-13，含极大前 2 天）",
		Peak:       model.Str("2026-08-13"),
		Days:       &days,
		Timezone:   "Asia/Shanghai",

		UTCOffsetHours: 8,
		Sites: []model.Site{
			{Name: "牵牛岗", Lat: 30.0260, Lon: 119.0070, Alt: 1489.9},
			{Name: "梅干岭", Lat: 30.1234, Lon: 119.2345, Alt: 1158},
		},
		HoursTotal:          18,
		HoursWithData:       9,
		VisibilityAvailable: false,
		GeneratedAt:         "2026-08-08 20:00:00",
		Source:              "Open-Meteo free API",
		Disclaimer:          "云底/云顶为气压层剖面反演值；天文量为纯 Python 近似算法结果。",
	}
}

func testRows() []model.HourRow {
	mk := func(site string, alt float64, iso string, hour int, night, rating, rel string,
		base, top float64) model.HourRow {
		return model.HourRow{
			Site: site, Lat: 30.0, Lon: 119.0, Alt: alt,
			TimeISO: iso, TimeShort: iso[5:10] + " " + iso[11:], Hour: hour,
			Night: night, HasData: true,
			LevelsTotal: 8, LevelsAbove: 3,
			CloudBaseMSL: model.Num(base + alt), CloudBaseAGL: model.Num(base),
			CloudTopMSL: model.Num(top + alt), CloudTopAGL: model.Num(top),
			CloudThickness: model.Num(top - base), LayerMaxCC: model.Num(88),
			Relation: model.Str(rel), Rating: rating, Note: "测试用例",
			CloudLow: model.Num(70), CloudLowSource: model.Str("model"),
			CloudMid: model.Num(10), CloudHigh: model.Num(5),
			Visibility: model.Missing(), Temp: model.Num(15.0), Dew: model.Num(12.5),
			Spread: model.Num(2.5), WindMS: model.Num(1.8),
			BoundaryLayerHeight: model.Num(300), FreezingLevelHeight: model.Num(4600),
			LCLAGLEst: model.Num(310),
			SunAlt:    model.Num(-31.2), MoonAlt: model.Num(-12.4),
			MoonIllum: model.Num(3), MoonPhase: "残月", GCAlt: model.Num(28.6),
			AstroDark: true,
			Layers: []model.LayerInfo{{
				BaseMSL: model.Num(base + alt), TopMSL: model.Num(top + alt),
				Thickness: model.Num(top - base), MaxCC: model.Num(88), MaxRH: model.Num(96),
			}},
		}
	}
	return []model.HourRow{
		mk("牵牛岗", 1489.9, "2026-08-13T23:00", 23, "2026-08-13", model.RATING_OK, model.REL_SEA_BELOW, -300, -120),
		mk("牵牛岗", 1489.9, "2026-08-14T00:00", 0, "2026-08-13", model.RATING_OK, model.REL_SEA_BELOW, -280, -100),
		mk("牵牛岗", 1489.9, "2026-08-14T01:00", 1, "2026-08-13", model.RATING_OK, model.REL_SEA_BELOW, -260, -90),
		mk("梅干岭", 1158, "2026-08-13T23:00", 23, "2026-08-13", model.RATING_BAD, model.REL_IN_CLOUD, -50, 400),
	}
}

func TestImpactWeightTotalIs100(t *testing.T) {
	factors := ImpactFactors(config.Default())
	if len(factors) != 9 {
		t.Fatalf("影响因素应为 9 项，实际 %d 项", len(factors))
	}
	if total := ImpactWeightTotal(factors); total != 100 {
		t.Fatalf("权重合计 = %d%%，必须为 100%%", total)
	}
	var autoW, manualW int
	for _, f := range factors {
		if !strings.HasPrefix(f.Source, SrcAuto) && !strings.HasPrefix(f.Source, SrcManual) {
			t.Fatalf("因素 %q 的来源标注既非「%s」也非「%s」：%q（诚实性红线）",
				f.Name, SrcAuto, SrcManual, f.Source)
		}
		if f.IsAuto() {
			autoW += f.Weight
		} else {
			manualW += f.Weight
		}
	}
	if autoW+manualW != 100 {
		t.Fatalf("自动(%d%%) + 人工(%d%%) != 100%%", autoW, manualW)
	}
	if manualW == 0 {
		t.Fatal("必须存在「需人工/外部数据」的因素，否则诚实性声明形同虚设")
	}
}

func TestReportFilename(t *testing.T) {
	meta := testMeta()
	if got := ReportFilename(meta); got != "astro_report_peak-2026-08-13.md" {
		t.Fatalf("peak 模式文件名 = %q", got)
	}
	if got := ExportFilename(meta, ".csv"); got != "astro_report_peak-2026-08-13.csv" {
		t.Fatalf("peak 模式 CSV 文件名 = %q", got)
	}
	if got := ExportFilename(meta, ".json"); got != "astro_report_peak-2026-08-13.json" {
		t.Fatalf("peak 模式 JSON 文件名 = %q", got)
	}

	meta.Peak = model.NullStr()
	meta.Days = nil
	if got := ReportFilename(meta); got != "astro_report_2026-08-11_2026-08-14.md" {
		t.Fatalf("区间模式文件名 = %q", got)
	}
	if got := ExportFilename(meta, ".csv"); got != "astro_report_2026-08-11_2026-08-14.csv" {
		t.Fatalf("区间模式 CSV 文件名 = %q", got)
	}
}

func TestBuildMarkdownReportSections(t *testing.T) {
	text := BuildMarkdownReport(testRows(), nil, testMeta(), config.Default())

	sections := []string{
		"# 山地星空 / 流星雨 低云海拔评估报告",
		"## 一、元信息",
		"### 1.1 点位列表",
		"## 二、各观测夜汇总",
		"### 2.1 天文条件（近似算法，取各点位经纬度均值）",
		"## 三、拍摄影响因素权重说明",
		"## 四、低云海拔评估明细",
		"## 五、导出字段说明（CSV / JSON 字段对照）",
	}
	pos := -1
	for _, s := range sections {
		idx := strings.Index(text, s)
		if idx < 0 {
			t.Fatalf("报告缺少章节 %q", s)
		}
		if idx < pos {
			t.Fatalf("章节 %q 顺序错乱", s)
		}
		pos = idx
	}
	if !strings.Contains(text, "**权重合计**：100%") {
		t.Fatal("报告未写出权重合计 100%")
	}

	if strings.Contains(text, "⛔ 本次运行没有取得任何有效预报") {
		t.Fatal("有有效数据却打出了无预报横幅")
	}

	for _, f := range CSVFields {
		if !strings.Contains(text, "| "+f+" |") {
			t.Fatalf("字段说明附录缺少字段 %q", f)
		}
	}
}

func TestBuildMarkdownReportNoForecast(t *testing.T) {
	rows := testRows()
	for i := range rows {
		rows[i].HasData = false
		rows[i].Rating = model.RATING_NODATA
	}
	text := BuildMarkdownReport(rows, nil, testMeta(), config.Default())
	if !strings.Contains(text, "⛔ 本次运行没有取得任何有效预报") {
		t.Fatal("全缺测时缺少无预报横幅")
	}
	if !strings.Contains(text, "## 三、拍摄影响因素权重说明") {
		t.Fatal("无预报时仍应保留权重章节（可先用于选点与器材准备）")
	}
	if !strings.Contains(text, "无数据") {
		t.Fatal("汇总矩阵应显示「无数据」")
	}
}

func TestMDCellEscapesPipe(t *testing.T) {
	if got := mdCell("云海在脚下|复核"); got != "云海在脚下\\|复核" {
		t.Fatalf("mdCell 未转义竖线：%q", got)
	}
	if got := mdCell("多行\n说明  压\t空白"); got != "多行 说明 压 空白" {
		t.Fatalf("mdCell 未压平空白：%q", got)
	}
	if got := mdCell(""); got != MissingCell {
		t.Fatalf("空值应显示 %q，实际 %q", MissingCell, got)
	}
}

func TestComputeSiteNightStats(t *testing.T) {
	cfg := config.Default()
	st := ComputeSiteNightStats("牵牛岗", "2026-08-13", testRows(), nil, cfg, [2]time.Time{})
	if st.Planned != 3 || st.Valid != 3 || st.Missing != 0 {
		t.Fatalf("时次统计 = planned %d / valid %d / missing %d", st.Planned, st.Valid, st.Missing)
	}
	if st.OK != 3 || st.Warn != 0 || st.Bad != 0 {
		t.Fatalf("评级统计 = ok %d / warn %d / bad %d", st.OK, st.Warn, st.Bad)
	}
	if st.DominantRelation != model.REL_SEA_BELOW {
		t.Fatalf("主要状态 = %q", st.DominantRelation)
	}
	if st.Verdict != "✅ 可去" {
		t.Fatalf("结论 = %q", st.Verdict)
	}
	if st.BestWindow != "23:00-02:00（3h）" {
		t.Fatalf("最佳连续窗口 = %q", st.BestWindow)
	}
	if !st.BaseAGLMin.Valid || st.BaseAGLMin.V != -300 || st.BaseAGLMax.V != -260 {
		t.Fatalf("云底 AGL 区间 = %+v ~ %+v", st.BaseAGLMin, st.BaseAGLMax)
	}

	empty := ComputeSiteNightStats("不存在的点位", "2026-08-13", testRows(), nil, cfg, [2]time.Time{})
	if empty.Valid != 0 || empty.Verdict != "❓ 无有效预报，请临近再跑" {
		t.Fatalf("空统计 = %+v", empty)
	}
	if empty.BaseAGLMin.Valid {
		t.Fatal("无数据时高度区间必须缺测，不能是 0")
	}
	if empty.MainReason != "" {
		t.Fatalf("无有效数据时主要诱因必须为空，实际 = %q", empty.MainReason)
	}
}

// 高山云海淹没机位（REL_SEA_BELOW_IN_CLOUD）且无浓雾时，报告「主要状态」必须呈现
// 「云海在脚下（机位在云中）」而非笼统的「机位在云中」，结论给 ⚠️ 而非 🔴。
func TestComputeSiteNightStatsSeaBelowInCloud(t *testing.T) {
	cfg := config.Default()
	mk := func(site string, hour int, night, rating, rel string, base, top float64) model.HourRow {
		return model.HourRow{
			Site: site, Hour: hour, Night: night, HasData: true,
			Relation: model.Str(rel), Rating: rating,
			CloudBaseAGL: model.Num(base), CloudTopAGL: model.Num(top),
		}
	}
	rows := []model.HourRow{
		mk("牛草山", 23, "2026-08-23", model.RATING_WARN, model.REL_SEA_BELOW_IN_CLOUD, -1389, 171),
		mk("牛草山", 0, "2026-08-23", model.RATING_WARN, model.REL_SEA_BELOW_IN_CLOUD, -1389, 171),
		mk("牛草山", 1, "2026-08-23", model.RATING_WARN, model.REL_SEA_BELOW_IN_CLOUD, -1389, 171),
	}
	st := ComputeSiteNightStats("牛草山", "2026-08-23", rows, nil, cfg, [2]time.Time{})

	if st.DominantRelation != model.REL_SEA_BELOW_IN_CLOUD {
		t.Fatalf("主要状态 = %q，want %q", st.DominantRelation, model.REL_SEA_BELOW_IN_CLOUD)
	}
	if label, ok := model.RelLabels[st.DominantRelation]; !ok || label != "云海在脚下（机位在云中）" {
		t.Fatalf("主要状态标签 = %q，want %q", label, "云海在脚下（机位在云中）")
	}
	if st.Verdict != "⚠️ 有机会但不稳定" {
		t.Fatalf("结论 = %q，want %q（非浓雾的高山云海应给机会，而非一律🔴）",
			st.Verdict, "⚠️ 有机会但不稳定")
	}
}

func TestReasonCategoryKeywords(t *testing.T) {
	cases := []struct {
		note string
		want string
	}{
		{"降水 2.5mm，不宜拍摄", "降水 / 雷暴"},
		{"雷暴天气（天气码 95），不宜拍摄", "降水 / 雷暴"},
		{"降水天气码 61，不宜拍摄", "降水 / 雷暴"},
		{"能见度 500m，辐射雾（静风 0.3m/s，天亮前最重）", "浓雾（能见度<1000m）"},
		{"能见度 200m，平流雾/低云压顶（风 1.2m/s）", "浓雾（能见度<1000m）"},
		{"近地RH 99%(代理判据)，辐射雾（静风 0.3m/s，天亮前最重）", "浓雾（能见度<1000m）"},
		{"机位在云中，无法拍摄（云顶还在头顶 250m）", "机位在云中"},
		{"云底在头顶 150m，成片遮挡（云量 75%）", "头顶厚云（云量≥70%）"},
		{"能见度 3000m，轻雾/霾", "轻雾/霾（1000–5000m）"},
		{"近地RH 96%(代理判据)，起雾风险", "轻雾/霾（1000–5000m）"},
		{"中云量 85%（3–8km，剖面之外），成片中云盖顶，星野受损", "中云盖顶（3–8km）"},
		{"高云量 92%（8km 以上卷云），头顶薄卷云，星野略受损", "高云洗天（8km+）"},
		{"云底在头顶 80m，按湿层判定（模式云量仅 35%），或仅为薄云", "头顶薄云（云量40–70%）"},
		{"温露差 1.5℃，镜头结露风险", "结露 / LCL 风险"},
		{"LCL≈120m(估算)，辐射雾风险高", "结露 / LCL 风险"},

		{"云海在脚下，头顶通透；近地RH 100%(代理判据)，辐射雾（静风 0.3m/s，天亮前最重）；温露差 0.0℃，镜头结露风险；LCL≈0m(估算)，辐射雾风险高", "浓雾（能见度<1000m）"},
		{"", ""},
		{"全层无云，头顶通透", ""},
	}
	for _, c := range cases {
		if got := reasonCategory(c.note); got != c.want {
			t.Errorf("reasonCategory(%q) = %q，want %q", c.note, got, c.want)
		}
	}
}

func TestMainReasonBadWinsOverWarn(t *testing.T) {
	cfg := config.Default()
	rows := []model.HourRow{

		mkReasonRow("牵牛岗", 1489.9, "2026-08-12T23:00", 23, "2026-08-12",
			model.RATING_BAD, model.REL_SEA_BELOW,
			"近地RH 99%(代理判据)，辐射雾（静风 0.3m/s，天亮前最重）"),
		mkReasonRow("牵牛岗", 1489.9, "2026-08-13T00:00", 0, "2026-08-12",
			model.RATING_BAD, model.REL_SEA_BELOW,
			"能见度 200m，辐射雾（静风 0.3m/s，天亮前最重）"),
		mkReasonRow("牵牛岗", 1489.9, "2026-08-13T01:00", 1, "2026-08-12",
			model.RATING_BAD, model.REL_SEA_BELOW,
			"能见度 100m，平流雾/低云压顶（风 1.2m/s）"),
		mkReasonRow("牵牛岗", 1489.9, "2026-08-13T02:00", 2, "2026-08-12",
			model.RATING_BAD, model.REL_SEA_BELOW,
			"近地RH 100%(代理判据)，辐射雾（静风 0.2m/s，天亮前最重）"),
		mkReasonRow("牵牛岗", 1489.9, "2026-08-13T03:00", 3, "2026-08-12",
			model.RATING_BAD, model.REL_SEA_BELOW,
			"能见度 150m，辐射雾（静风 0.3m/s，天亮前最重）"),
	}
	st := ComputeSiteNightStats("牵牛岗", "2026-08-12", rows, nil, cfg, [2]time.Time{})
	if st.OK != 0 || st.Warn != 0 || st.Bad != 5 {
		t.Fatalf("统计口径错：ok=%d warn=%d bad=%d", st.OK, st.Warn, st.Bad)
	}
	if st.Verdict != "🔴 建议放弃该点位" {
		t.Fatalf("结论 = %q", st.Verdict)
	}

	if st.MainReason != "浓雾（能见度<1000m）" {
		t.Fatalf("主要诱因 = %q，want %q（用户原话：「云海在脚下 + 🔴建议放弃」必须解释清楚）",
			st.MainReason, "浓雾（能见度<1000m）")
	}
}

func TestMainReasonHardVetoWinsOverLCL(t *testing.T) {
	cfg := config.Default()
	rows := []model.HourRow{
		mkReasonRow("X", 1400, "2026-08-12T23:00", 23, "2026-08-12",
			model.RATING_BAD, model.REL_SEA_BELOW,
			"云海在脚下，头顶通透；近地RH 100%(代理判据)，辐射雾（静风 0.3m/s，天亮前最重）；温露差 0.0℃，镜头结露风险；LCL≈0m(估算)，辐射雾风险高"),
		mkReasonRow("X", 1400, "2026-08-13T00:00", 0, "2026-08-12",
			model.RATING_BAD, model.REL_SEA_BELOW,
			"云海在脚下，头顶通透；近地RH 99%(代理判据)，辐射雾（静风 0.2m/s，天亮前最重）；温露差 0.5℃，镜头结露风险；LCL≈30m(估算)，辐射雾风险高"),
		mkReasonRow("X", 1400, "2026-08-13T01:00", 1, "2026-08-12",
			model.RATING_BAD, model.REL_SEA_BELOW,
			"云海在脚下，头顶通透；能见度 300m，辐射雾（静风 0.4m/s，天亮前最重）；温露差 1.0℃，镜头结露风险；LCL≈60m(估算)，辐射雾风险高"),
		mkReasonRow("X", 1400, "2026-08-13T02:00", 2, "2026-08-12",
			model.RATING_BAD, model.REL_IN_CLOUD,
			"机位在云中，无法拍摄；降水 2.0mm，降水天气码 63，不宜拍摄"),
	}
	st := ComputeSiteNightStats("X", "2026-08-12", rows, nil, cfg, [2]time.Time{})
	if st.MainReason != "浓雾（能见度<1000m）" {
		t.Fatalf("主要诱因 = %q，want %q（硬否决必须覆盖软辅助的结露/LCL）",
			st.MainReason, "浓雾（能见度<1000m）")
	}
}

func TestMainReasonSeverityTiebreak(t *testing.T) {
	cfg := config.Default()
	rows := []model.HourRow{
		mkReasonRow("A", 1000, "2026-08-12T22:00", 22, "2026-08-12",
			model.RATING_BAD, model.REL_OVERHEAD,
			"云底在头顶 80m，按湿层判定（模式云量仅 35%），或仅为薄云"),
		mkReasonRow("A", 1000, "2026-08-12T23:00", 23, "2026-08-12",
			model.RATING_BAD, model.REL_OVERHEAD,
			"云底在头顶 80m，按湿层判定（模式云量仅 35%），或仅为薄云"),
		mkReasonRow("A", 1000, "2026-08-13T00:00", 0, "2026-08-12",
			model.RATING_BAD, model.REL_IN_CLOUD,
			"机位在云中，无法拍摄（云顶还在头顶 250m）"),
		mkReasonRow("A", 1000, "2026-08-13T01:00", 1, "2026-08-12",
			model.RATING_BAD, model.REL_IN_CLOUD,
			"机位在云中，无法拍摄（云顶还在头顶 250m）"),
	}
	st := ComputeSiteNightStats("A", "2026-08-12", rows, nil, cfg, [2]time.Time{})

	if st.MainReason != "机位在云中" {
		t.Fatalf("同频次严重度 tie-break 失败：%q", st.MainReason)
	}
}

func TestMainReasonAllOKReturnsEmpty(t *testing.T) {
	cfg := config.Default()
	rows := []model.HourRow{
		mkReasonRow("OK", 1000, "2026-08-12T22:00", 22, "2026-08-12",
			model.RATING_OK, model.REL_SEA_BELOW, "云海在脚下，头顶通透，最佳拍摄条件"),
		mkReasonRow("OK", 1000, "2026-08-12T23:00", 23, "2026-08-12",
			model.RATING_OK, model.REL_SEA_BELOW, "云海在脚下，头顶通透，最佳拍摄条件"),
	}
	st := ComputeSiteNightStats("OK", "2026-08-12", rows, nil, cfg, [2]time.Time{})
	if st.MainReason != "" {
		t.Fatalf("全 OK 时主要诱因必须为空，实际 %q", st.MainReason)
	}
}

func TestMainReasonBadWinsOverMoreFrequentWarn(t *testing.T) {
	cfg := config.Default()
	rows := []model.HourRow{

		mkReasonRow("X", 1000, "2026-08-12T23:00", 23, "2026-08-12",
			model.RATING_BAD, model.REL_OVERHEAD,
			"降水 2.5mm，不宜拍摄"),
		mkReasonRow("X", 1000, "2026-08-13T00:00", 0, "2026-08-12",
			model.RATING_WARN, model.REL_OVERHEAD,
			"中云量 85%（3–8km，剖面之外），成片中云盖顶，星野受损"),
		mkReasonRow("X", 1000, "2026-08-13T01:00", 1, "2026-08-12",
			model.RATING_WARN, model.REL_OVERHEAD,
			"高云量 92%（8km 以上卷云），头顶薄卷云，星野略受损"),
		mkReasonRow("X", 1000, "2026-08-13T02:00", 2, "2026-08-12",
			model.RATING_WARN, model.REL_OVERHEAD,
			"中云量 88%（3–8km，剖面之外），成片中云盖顶，星野受损"),
		mkReasonRow("X", 1000, "2026-08-13T03:00", 3, "2026-08-12",
			model.RATING_WARN, model.REL_OVERHEAD,
			"高云量 95%（8km 以上卷云），头顶薄卷云，星野略受损"),
	}
	st := ComputeSiteNightStats("X", "2026-08-12", rows, nil, cfg, [2]time.Time{})
	if st.MainReason != "降水 / 雷暴" {
		t.Fatalf("1 BAD 降水 vs 4 WARN 薄云：主要诱因必须报降水（硬否决），实际 %q", st.MainReason)
	}
}

func TestMainReasonFallsBackToWarn(t *testing.T) {
	cfg := config.Default()
	rows := []model.HourRow{
		mkReasonRow("W", 1000, "2026-08-12T22:00", 22, "2026-08-12",
			model.RATING_WARN, model.REL_OVERHEAD, "中云量 85%（3–8km，剖面之外），成片中云盖顶，星野受损"),
		mkReasonRow("W", 1000, "2026-08-12T23:00", 23, "2026-08-12",
			model.RATING_WARN, model.REL_OVERHEAD, "中云量 85%（3–8km，剖面之外），成片中云盖顶，星野受损"),
	}
	st := ComputeSiteNightStats("W", "2026-08-12", rows, nil, cfg, [2]time.Time{})
	if st.MainReason != "中云盖顶（3–8km）" {
		t.Fatalf("仅 WARN 时应统计 WARN，实际 %q", st.MainReason)
	}
}

// TestCrossModelCoreWindowCountsConsensusHours 回归：核心窗口共识统计必须用整点字段，
// 不能再用 parseHourShort(c.TimeShort)——TimeShort 形如 "08-12 23:00"（日期前缀），
// 旧实现会把前两位 "08" 当成小时，导致所有共识时次落在核心窗外、CrossOK 恒为 0。
func TestCrossModelCoreWindowCountsConsensusHours(t *testing.T) {
	cfg := config.Default() // 核心窗口 23:00–05:00

	// 7 个整点的 HourRow（仅用于撑起 Valid 计数，评级不影响 CrossOK）
	rows := make([]model.HourRow, 0, 7)
	hours := []int{23, 0, 1, 2, 3, 4, 5}
	for _, h := range hours {
		iso := fmt.Sprintf("2026-08-12T%02d:00", h)
		rows = append(rows, mkReasonRow("K", 900, iso, h, "2026-08-12",
			model.RATING_OK, model.REL_SEA_BELOW, "云海在脚下，头顶通透"))
	}

	// 逐小时 consensus 配对：23:00 与 01:00 两模型都通透，其余两模型都不宜。
	// TimeShort 故意用 "08-12 HH:00" 格式（日期前缀），复现旧 bug 触发条件。
	compare := make([]model.ModelCompareRow, 0, 7)
	for i, h := range hours {
		cons := model.ConsensusBothBad
		if h == 23 || h == 1 {
			cons = model.ConsensusBothOK
		}
		compare = append(compare, model.ModelCompareRow{
			Site:       "K",
			Night:      "2026-08-12",
			TimeISO:    fmt.Sprintf("2026-08-12T%02d:00", h),
			TimeShort:  fmt.Sprintf("08-12 %02d:00", h), // 含日期前缀，旧实现会误解析
			Hour:       h,
			IconRating: model.RATING_OK,
			GfsRating:  model.RATING_OK,
			Consensus:  cons,
		})
		_ = i
	}

	st := ComputeSiteNightStats("K", "2026-08-12", rows, compare, cfg, [2]time.Time{})
	if st.CrossOK != 2 {
		t.Fatalf("核心窗口共识时次数 = %d, want 2（23:00 与 01:00 两模型都通透）；"+
			"旧实现会把 TimeShort %q 解析成小时 8 而漏判全部共识时次", st.CrossOK, "08-12 23:00")
	}
}

func mkReasonRow(site string, alt float64, iso string, hour int, night,
	rating, rel string, note string) model.HourRow {
	return model.HourRow{
		Site: site, Lat: 30.0, Lon: 119.0, Alt: alt,
		TimeISO: iso, TimeShort: iso[5:10] + " " + iso[11:], Hour: hour,
		Night: night, HasData: true,
		LevelsTotal: 8, LevelsAbove: 3,
		CloudBaseMSL: model.Num(-300 + alt), CloudBaseAGL: model.Num(-300),
		CloudTopMSL: model.Num(-100 + alt), CloudTopAGL: model.Num(-100),
		Relation: model.Str(rel), Rating: rating, Note: note,
		Visibility: model.Missing(), Temp: model.Num(15.0), Dew: model.Num(12.5),
		Spread: model.Num(2.5), WindMS: model.Num(1.8),
		AstroDark: true,
	}
}

func TestExportCSVHeaderAndBOM(t *testing.T) {
	if len(CSVFields) != 31 {
		t.Fatalf("CSV 字段数 = %d，want 31", len(CSVFields))
	}
	for _, f := range CSVFields {
		if _, ok := FieldLabels[f]; !ok {
			t.Fatalf("字段 %q 缺少中文标签", f)
		}
		if _, ok := FieldNotes[f]; !ok {
			t.Fatalf("字段 %q 缺少说明", f)
		}
	}

	path := filepath.Join(t.TempDir(), "out.csv")
	if err := ExportCSV(path, testRows()); err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 3 || data[0] != 0xEF || data[1] != 0xBB || data[2] != 0xBF {
		t.Fatal("CSV 缺少 UTF-8 BOM，Excel 打开会乱码")
	}
	text := string(data[3:])
	if !strings.Contains(text, "\r\n") {
		t.Fatal("CSV 未使用 CRLF 行结束符")
	}
	lines := strings.Split(strings.TrimRight(text, "\r\n"), "\r\n")
	if len(lines) != len(testRows())+1 {
		t.Fatalf("CSV 行数 = %d，want %d（表头 + 数据）", len(lines), len(testRows())+1)
	}
	header := strings.Split(lines[0], ",")
	if len(header) != 31 {
		t.Fatalf("表头列数 = %d，want 31", len(header))
	}
	if header[0] != "点位" || header[4] != "有数据" || header[30] != "判断说明" {
		t.Fatalf("表头不是中文标签：%v", header[:5])
	}

	if !strings.HasPrefix(lines[1], "梅干岭,1158.0,") {
		t.Fatalf("数据行排序或格式不符：%q", lines[1])
	}

	if strings.Contains(lines[1], ",-,") {
		t.Fatalf("CSV 里出现了终端占位符 \"-\"：%q", lines[1])
	}
	if !strings.Contains(lines[1], ",True,") {
		t.Fatalf("布尔值应写成 Python 风格 True：%q", lines[1])
	}
}

func TestBuildJSONShape(t *testing.T) {
	data, err := BuildJSON(testMeta(), testRows(), config.Default())
	if err != nil {
		t.Fatalf("序列化失败：%v", err)
	}

	var payload struct {
		FieldLabels map[string]string `json:"field_labels"`
		Meta        struct {
			Models string `json:"models"`
			Peak   string `json:"peak"`
		} `json:"meta"`
		Config map[string]any `json:"config"`
		Rows   []struct {
			Site       string   `json:"site"`
			Visibility *float64 `json:"visibility"`
			Rating     string   `json:"rating"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("产物不是合法 JSON：%v", err)
	}
	if len(payload.FieldLabels) != 31 {
		t.Fatalf("field_labels 条目数 = %d，want 31", len(payload.FieldLabels))
	}
	if payload.Meta.Models != "icon_seamless" || payload.Meta.Peak != "2026-08-13" {
		t.Fatalf("meta 不完整：%+v", payload.Meta)
	}
	if len(payload.Rows) != 4 {
		t.Fatalf("rows 条目数 = %d", len(payload.Rows))
	}
	if payload.Rows[0].Visibility != nil {
		t.Fatal("缺测的能见度应导出为 null，而不是 0")
	}

	for k := range payload.Config {
		if strings.HasPrefix(k, "cache") {
			t.Fatalf("config 段不应包含缓存键 %q", k)
		}
	}
	for _, k := range []string{"cloud_cover_threshold", "fog_visibility_m",
		"night_start_hour", "core_end_hour", "astro_dark_sun_alt", "retries"} {
		if _, ok := payload.Config[k]; !ok {
			t.Fatalf("config 段缺少键 %q", k)
		}
	}

	if strings.Contains(string(data), `\u`) {
		t.Fatal("JSON 里出现了 \\u 转义，中文应直接可读")
	}
	if !strings.Contains(string(data), "牵牛岗") {
		t.Fatal("JSON 里找不到中文点位名")
	}
}

func TestExportJSONWritesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := ExportJSON(path, testMeta(), testRows(), config.Default()); err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Fatalf("落盘的 JSON 不可解析：%v", err)
	}
	for _, k := range []string{"field_labels", "meta", "config", "rows"} {
		if _, ok := probe[k]; !ok {
			t.Fatalf("落盘 JSON 缺少顶层键 %q", k)
		}
	}
}

func TestWriteMarkdownReportCreatesDir(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "reports", "nested")
	path, err := WriteMarkdownReport(testRows(), nil, testMeta(), config.Default(), outDir)
	if err != nil {
		t.Fatalf("写报告失败：%v", err)
	}
	if filepath.Base(path) != "astro_report_peak-2026-08-13.md" {
		t.Fatalf("报告文件名 = %q", filepath.Base(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# 山地星空") {
		t.Fatalf("报告开头异常：%q", string(data[:40]))
	}
}

func TestComputeNightAstroRespectsUTCOffset(t *testing.T) {
	cfg := config.Default()
	sites := []model.Site{
		{Name: "东京点", Lat: 35.6762, Lon: 139.6503, Alt: 50},
	}
	night := "2026-08-13"

	base, err := time.ParseInLocation("2006-01-02T15:04", night+"T22:00", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	offset9 := 9 * 3600
	darkSunAlt := cfg.Thresh.AstroDarkSunAlt
	lat, lon := 35.6762, 139.6503

	expMoonUp, expDark := 0, 0
	for h := 0; h <= 8; h++ {
		a := astro.Compute(base.Add(time.Duration(h)*time.Hour), offset9, lat, lon, darkSunAlt)
		if a.AstroDark {
			expDark++
		}
		if a.MoonAlt > 0 {
			expMoonUp++
		}
	}
	st := ComputeNightAstro(night, sites, cfg, offset9)
	if st.MoonUpHours != expMoonUp {
		t.Fatalf("+9 偏移下 MoonUpHours = %d，want %d（应为逐时口径，而非北京时间）",
			st.MoonUpHours, expMoonUp)
	}
	if st.DarkHours != expDark {
		t.Fatalf("+9 偏移下 DarkHours = %d，want %d（应为逐时口径，而非北京时间）",
			st.DarkHours, expDark)
	}

	expMoonUp8, expDark8 := 0, 0
	for h := 0; h <= 8; h++ {
		a := astro.Compute(base.Add(time.Duration(h)*time.Hour), 8*3600, lat, lon, darkSunAlt)
		if a.AstroDark {
			expDark8++
		}
		if a.MoonAlt > 0 {
			expMoonUp8++
		}
	}
	if expMoonUp != expMoonUp8 || expDark != expDark8 {
		st8 := ComputeNightAstro(night, sites, cfg, 8*3600)
		if st.MoonUpHours == st8.MoonUpHours && st.DarkHours == st8.DarkHours {
			t.Fatal("ComputeNightAstro 忽略了 utcOffsetSec 参数（两个偏移给出相同结果）—— 疑似仍硬编码 +8")
		}
	}
}

func TestLongestRunDetectsGap(t *testing.T) {
	mk := func(iso string, hour int) model.HourRow {
		return model.HourRow{TimeISO: iso, Hour: hour, Rating: model.RATING_OK, HasData: true}
	}

	rows := []model.HourRow{
		mk("2026-08-13T23:00", 23),
		mk("2026-08-14T00:00", 0),
		mk("2026-08-14T01:00", 1),

		mk("2026-08-14T03:00", 3),
		mk("2026-08-14T04:00", 4),
	}
	got := LongestRun(rows, model.RATING_OK)
	if want := "23:00-02:00（3h）"; got != want {
		t.Fatalf("最长连续窗口 = %q，want %q（不应把跳过 02:00 的两段缝合）", got, want)
	}
	if got == "23:00-04:00（5h）" {
		t.Fatal("LongestRun 把缺测缺口缝合了，虚增了窗口时长")
	}

	rows2 := []model.HourRow{
		mk("2026-08-13T23:00", 23),

		mk("2026-08-14T01:00", 1),
		mk("2026-08-14T02:00", 2),
		mk("2026-08-14T03:00", 3),
	}
	if got2 := LongestRun(rows2, model.RATING_OK); got2 != "01:00-04:00（3h）" {
		t.Fatalf("晚段最长时 = %q，want %q", got2, "01:00-04:00（3h）")
	}
}

func TestPrintTomorrowHeaderNoCityName(t *testing.T) {
	var buf strings.Builder
	PrintTomorrowHeader(&buf, testMeta(), config.Default())
	out := buf.String()

	if strings.Contains(out, "Asia/Shanghai") {
		t.Fatalf("B 轨页首仍含具体城市名 Asia/Shanghai：\n%s", out)
	}
	if strings.Contains(out, "北京时间") {
		t.Fatalf("B 轨页首仍硬编码城市名「北京时间」，应改为偏移推导的 UTC 标签：\n%s", out)
	}
	if !strings.Contains(out, "UTC+") {
		t.Fatalf("B 轨页首时区未改为 UTC 偏移标签（应含 UTC+）：\n%s", out)
	}
}

func TestTomorrowVerdictMixedReasonsShowsAmbiguousTip(t *testing.T) {
	st := TomorrowSiteNightStats{
		Judgeable:      0,
		NoData:         2,
		QuotaExhausted: false,
		Reasons: []TomorrowReasonCount{
			{
				Reason: dualtrack.RoundQuotaDown,
				Label:  dualtrack.NoDataReasonLabels[dualtrack.RoundQuotaDown],
				Count:  1,
			},
			{
				Reason: dualtrack.AmbiguousBase,
				Label:  dualtrack.NoDataReasonLabels[dualtrack.AmbiguousBase],
				Count:  1,
			},
		},
	}
	got := tomorrowVerdict(st)
	if !strings.Contains(got, "全夜云底低于机位") {
		t.Fatalf("混合归因（配额耗尽 + 云底低于机位）下未给出专指提示，结论 = %q", got)
	}
	if strings.Contains(got, "配额耗尽") {
		t.Fatalf("AmbiguousBase 命中时不应退回到配额文案，结论 = %q", got)
	}
}

func TestAnnotateRelation(t *testing.T) {
	cases := []struct {
		rel, reason, want string
	}{
		{"云海在脚下", "浓雾（能见度<1000m）", "云海在脚下（浓雾/降水遮蔽，不可见）"},
		{"云海在脚下", "降水 / 雷暴", "云海在脚下（浓雾/降水遮蔽，不可见）"},
		{"云海在脚下", "轻雾/霾（1000–5000m）", "云海在脚下"},
		{"云海在脚下", "头顶薄云（云量40–70%）", "云海在脚下"},
		{"云海在脚下", "高云洗天（8km+）", "云海在脚下"},
		{"云海在脚下", "", "云海在脚下"},
		{"机位在云中", "浓雾（能见度<1000m）", "机位在云中"},
		{"头顶通透", "降水 / 雷暴", "头顶通透"},
	}
	for _, c := range cases {
		if got := annotateRelation(c.rel, c.reason); got != c.want {
			t.Errorf("annotateRelation(%q,%q) = %q, want %q", c.rel, c.reason, got, c.want)
		}
	}
}

// TestRelationLabelClearWithInCloud 验证方案 A：众数为全层无云但存在机位在云中时次时，
// 「主要状态」列加注限定，避免"全层无云"被误读为整夜干净。
func TestRelationLabelClearWithInCloud(t *testing.T) {
	// 众数无云 + 2 个机位在云中时次 → 加注限定
	st := SiteNightStats{DominantRelation: model.REL_CLEAR, InCloudHours: 2}
	if got := RelationLabel(st); got != "全层无云（多数时次，2 时次机位在云中）" {
		t.Fatalf("RelationLabel = %q，want 限定版", got)
	}
	// 众数无云且无埋云时次 → 纯净标签
	if got := RelationLabel(SiteNightStats{DominantRelation: model.REL_CLEAR}); got != "全层无云" {
		t.Fatalf("RelationLabel(无埋云) = %q，want 全层无云", got)
	}
	// 众数非无云 → 原标签，绝不限定
	st3 := SiteNightStats{DominantRelation: model.REL_IN_CLOUD, InCloudHours: 3}
	if got := RelationLabel(st3); got != "机位在云中" {
		t.Fatalf("RelationLabel(众数埋云) = %q，want 机位在云中", got)
	}
	// 高山云海淹没机位：标签本身已含"机位在云中"，不重复限定
	if got := RelationLabel(SiteNightStats{DominantRelation: model.REL_SEA_BELOW_IN_CLOUD, InCloudHours: 2}); got != "云海在脚下（机位在云中）" {
		t.Fatalf("RelationLabel(云海淹没) = %q，want 云海在脚下（机位在云中）", got)
	}
	// 无有效关系 → 缺测占位
	if got := RelationLabel(SiteNightStats{}); got != MissingCell {
		t.Fatalf("RelationLabel(空) = %q，want %q", got, MissingCell)
	}
}

// TestComputeSiteNightStatsClearDominatesButInCloud 端到端验证：9-06 夜形态
// （多数时次全层无云，但有少数时次机位埋云）下，DominantRelation / InCloudHours /
// RelationLabel 三者联动正确。
func TestComputeSiteNightStatsClearDominatesButInCloud(t *testing.T) {
	mk := func(hour int, rating, rel string) model.HourRow {
		return model.HourRow{
			Site: "廿四尖", Lat: 28.95, Lon: 120.66, Alt: 1218.3,
			TimeISO: fmt.Sprintf("2026-09-06T%02d:00", hour), Hour: hour,
			Night: "2026-09-06", HasData: true,
			LevelsTotal: 8, LevelsAbove: 3,
			Relation: model.Str(rel), Rating: rating, Note: "测试",
			CloudLow: model.Num(70), CloudLowSource: model.Str("model"),
			CloudMid: model.Num(10), CloudHigh: model.Num(5),
			WindMS: model.Num(1.8), BoundaryLayerHeight: model.Num(300),
		}
	}
	rows := []model.HourRow{
		mk(22, model.RATING_OK, model.REL_CLEAR),
		mk(23, model.RATING_OK, model.REL_CLEAR),
		mk(0, model.RATING_OK, model.REL_CLEAR),
		mk(1, model.RATING_OK, model.REL_CLEAR),
		mk(2, model.RATING_BAD, model.REL_IN_CLOUD),
		mk(3, model.RATING_BAD, model.REL_IN_CLOUD),
		mk(4, model.RATING_WARN, model.REL_OVERHEAD),
	}
	st := ComputeSiteNightStats("廿四尖", "2026-09-06", rows, nil, config.Default(), [2]time.Time{})
	if st.DominantRelation != model.REL_CLEAR {
		t.Fatalf("DominantRelation = %q，want REL_CLEAR（全层无云为众数）", st.DominantRelation)
	}
	if st.InCloudHours != 2 {
		t.Fatalf("InCloudHours = %d，want 2", st.InCloudHours)
	}
	if got := RelationLabel(st); got != "全层无云（多数时次，2 时次机位在云中）" {
		t.Fatalf("主要状态标签 = %q，want 限定版", got)
	}
}
