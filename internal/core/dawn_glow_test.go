package core

import (
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// TestAssessDawnGlow_StructuralGuard 验证结构护栏：满云（>=90%）原口径会盲目判大烧，
// 实际是不可染的厚云层，应封顶中烧；普通大烧区间不受影响（回归 TestAssessDawnGlow）。
func TestAssessDawnGlow_StructuralGuard(t *testing.T) {
	got, note := assessDawnGlow(10, 95, 92)
	if got != "中烧" {
		t.Fatalf("满云应封顶中烧，实际 %q", got)
	}
	if !strings.Contains(note, "厚云层") {
		t.Errorf("封顶备注未说明厚云层：%q", note)
	}
	if g, _ := assessDawnGlow(10, 50, 40); g != "大烧" {
		t.Fatalf("midhigh=50 应大烧，实际 %q", g)
	}
}

// TestDegradeDawnGlowByAOD 锁死 AOD 降级阈值：>=1.0→无，>=0.6→小烧，>=0.3→中烧，缺失不降级。
func TestDegradeDawnGlowByAOD(t *testing.T) {
	if g, _ := degradeDawnGlowByAOD("大烧", "", model.Num(1.2)); g != "无" {
		t.Fatalf("AOD>=1.0 应判无，实际 %q", g)
	}
	if g, _ := degradeDawnGlowByAOD("大烧", "", model.Num(0.4)); g != "中烧" {
		t.Fatalf("AOD>=0.3 应封顶中烧，实际 %q", g)
	}
	if g, _ := degradeDawnGlowByAOD("大烧", "", model.Num(0.7)); g != "小烧" {
		t.Fatalf("AOD>=0.6 应封顶小烧，实际 %q", g)
	}
	// 低 AOD 不降级（0.2 < 0.3 阈值）
	if g, _ := degradeDawnGlowByAOD("大烧", "原备注", model.Num(0.2)); g != "大烧" {
		t.Fatalf("AOD 0.2 不应降级，实际 %q", g)
	}
	// 缺失不降级
	if g, _ := degradeDawnGlowByAOD("大烧", "原备注", model.Missing()); g != "大烧" {
		t.Fatalf("AOD 缺失不应降级，实际 %q", g)
	}
}

// TestConsensusDawnGlow_ICONOutlierDowngraded 复现绩溪 9-06 根因：
// 主模式(默认 ICON)报大烧，但 GFS/ECMWF/best_match 三个模型 小烧/无/无 →
// 多模型共识应把大烧封顶为无（与 sunsetbot「不烧」对齐），并标注分歧。
func TestConsensusDawnGlow_ICONOutlierDowngraded(t *testing.T) {
	compare := map[string]string{
		"gfs_seamless": "小烧",
		"ecmwf_ifs025": "无",
		"best_match":   "无",
	}
	final, note, divergent := consensusDawnGlow("大烧", "中高云量 40% 适中", compare)
	if final != "无" {
		t.Fatalf("ICON 离群应被封顶为无（对齐 sunsetbot 不烧），实际 %q", final)
	}
	if !divergent {
		t.Fatal("应当判定为分歧降级")
	}
	if !strings.Contains(note, "模型分歧") {
		t.Errorf("备注应标注模型分歧：%q", note)
	}
}

// TestConsensusDawnGlow_SingleModelNoChange 单模型（无对比）必须原样返回，保证回归安全。
func TestConsensusDawnGlow_SingleModelNoChange(t *testing.T) {
	final, _, divergent := consensusDawnGlow("大烧", "x", nil)
	if final != "大烧" || divergent {
		t.Fatalf("单模型应原样返回，实际 final=%q divergent=%v", final, divergent)
	}
}

// TestConsensusDawnGlow_MajorityBigStillBig 多数模型都报大烧时不降级。
func TestConsensusDawnGlow_MajorityBigStillBig(t *testing.T) {
	compare := map[string]string{"gfs": "大烧", "ecmwf": "大烧", "best": "中烧"}
	final, _, divergent := consensusDawnGlow("大烧", "x", compare)
	if final != "大烧" || divergent {
		t.Fatalf("多数大烧不应降级，实际 final=%q divergent=%v", final, divergent)
	}
}

// TestConsensusDawnGlow_MajorityNoCapsBig 2/4 大烧、2/4 无（势均）时，
// 大烧未达多数，应封顶到共识档位（无）。
func TestConsensusDawnGlow_MajorityNoCapsBig(t *testing.T) {
	compare := map[string]string{"gfs": "大烧", "ecmwf": "无", "best": "无"}
	final, _, divergent := consensusDawnGlow("大烧", "x", compare)
	if final != "无" || !divergent {
		t.Fatalf("2/4 大烧未达多数应封顶无，实际 final=%q divergent=%v", final, divergent)
	}
}

// TestBuildSunriseReport_DawnGlowTransparency 验证「分歧透明化」：
// 共识模式(多模型)下，报告须把主模型 + 各对比模型的原始档位都摊开到 DawnGlowModels，
// 并给出分歧描述；单模型(DawnGlowContext{})下两者为空，保证回归安全。
func TestBuildSunriseReport_DawnGlowTransparency(t *testing.T) {
	cfg := config.Default()
	night := mergeNight
	sunriseDate := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	resp := makeCloudSeaResp(t)

	// 多模型：主模型大烧，对比模型 小烧/无/无（绩溪式离群）
	ctx := DawnGlowContext{
		PrimaryModel: "icon_seamless",
		Compare: map[string]string{
			"gfs_seamless": "小烧",
			"ecmwf_ifs025": "无",
			"best_match":   "无",
		},
	}
	res := BuildSunriseReport(mergeSite, resp, night, sunriseDate, cfg, 28800, 30, ctx)
	if len(res.DawnGlowModels) != 4 {
		t.Fatalf("DawnGlowModels 应有 4 条（主+3对比），实际 %d: %+v", len(res.DawnGlowModels), res.DawnGlowModels)
	}
	if !res.DawnGlowModels[0].Primary || res.DawnGlowModels[0].Model != "icon_seamless" {
		t.Errorf("首条应为带 Primary 标记的主模型，实际 %+v", res.DawnGlowModels[0])
	}
	if res.DawnGlowDivergence == "" || !strings.Contains(res.DawnGlowDivergence, "分歧") {
		t.Errorf("多模型下应给出含「分歧」的描述，实际 %q", res.DawnGlowDivergence)
	}

	// 单模型回归安全：不填充明细
	res1 := BuildSunriseReport(mergeSite, resp, night, sunriseDate, cfg, 28800, 30, DawnGlowContext{})
	if len(res1.DawnGlowModels) != 0 {
		t.Errorf("单模型下 DawnGlowModels 应为空，实际 %d", len(res1.DawnGlowModels))
	}
	if res1.DawnGlowDivergence != "" {
		t.Errorf("单模型下 DawnGlowDivergence 应为空，实际 %q", res1.DawnGlowDivergence)
	}
}

// TestLooseDawnGlow 直接锁死宽松口径策略函数：主模型(ICON)与 ECMWF 取高，
// 任一报大烧即采纳；无 ECMWF（单模型）时退回主模型结论。
func TestLooseDawnGlow(t *testing.T) {
	// ICON=中烧, ECMWF=大烧 → 取高 大烧
	if g, _ := looseDawnGlow("中烧", "icon_seamless", map[string]string{"ecmwf_ifs025": "大烧"}); g != "大烧" {
		t.Fatalf("ECMWF 大烧应取高，实际 %q", g)
	}
	// ICON=大烧, ECMWF=无 → 保留主模型 大烧（不被压低）
	if g, _ := looseDawnGlow("大烧", "icon_seamless", map[string]string{"ecmwf_ifs025": "无"}); g != "大烧" {
		t.Fatalf("主模型大烧应保留，实际 %q", g)
	}
	// ICON=无, ECMWF=小烧 → 取高 小烧
	if g, _ := looseDawnGlow("无", "icon_seamless", map[string]string{"ecmwf_ifs025": "小烧"}); g != "小烧" {
		t.Fatalf("ECMWF 小烧应取高，实际 %q", g)
	}
	// 单模型（无 ecmwf）→ 退回主模型
	if g, _ := looseDawnGlow("中烧", "icon_seamless", nil); g != "中烧" {
		t.Fatalf("无对比应退回主模型，实际 %q", g)
	}
}

// TestBuildSunriseReport_GlowPolicyLoose 锁死 --glow-policy loose 接线：
// 同一份输入（主模型因低云 60% 判无，ECMWF 判大烧），共识模式封顶为无，
// 但 loose 取主ICON与ECMWF最高档 → 大烧。证明 BuildSunriseReport 确实按
// GlowPolicy 分支切换到 looseDawnGlow，而非一律走共识。
func TestBuildSunriseReport_GlowPolicyLoose(t *testing.T) {
	cfg := config.Default()
	night := mergeNight
	sunriseDate := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	resp := makeCloudSeaResp(t)

	compare := map[string]string{
		"gfs_seamless": "小烧",
		"ecmwf_ifs025": "大烧",
		"best_match":   "无",
	}

	// 共识模式：主模型低云 60% 判无，ECMWF 大烧被多数封顶 → 无
	resC := BuildSunriseReport(mergeSite, resp, night, sunriseDate, cfg, 28800, 30,
		DawnGlowContext{PrimaryModel: "icon_seamless", Compare: compare})
	if resC.DawnGlow != "无" {
		t.Fatalf("共识模式应封顶为无，实际 %q", resC.DawnGlow)
	}

	// loose 模式：取主ICON与ECMWF最高档 → 大烧
	resL := BuildSunriseReport(mergeSite, resp, night, sunriseDate, cfg, 28800, 30,
		DawnGlowContext{PrimaryModel: "icon_seamless", Compare: compare, GlowPolicy: "loose"})
	if resL.DawnGlow != "大烧" {
		t.Fatalf("loose 模式应取 ECMWF 大烧 → 大烧，实际 %q", resL.DawnGlow)
	}
	if !strings.Contains(resL.DawnGlowNote, "宽松口径") {
		t.Errorf("loose 备注应标注宽松口径，实际 %q", resL.DawnGlowNote)
	}
}

// TestSunsetDawnGlow_ICONOutlierIgnored 复现绩溪 9-06 根因（sunset 口径）：
// ICON 报大烧，但 GFS 小烧 / ECMWF 无 / best 无 → 以 GFS/ECMWF 取低，
// 排除 ICON 离群，对齐 sunsetbot 的「不烧」结论。
func TestSunsetDawnGlow_ICONOutlierIgnored(t *testing.T) {
	models := map[string]string{
		"icon_seamless": "大烧", "gfs_seamless": "小烧",
		"ecmwf_ifs025": "无", "best_match": "无",
	}
	if g, _ := sunsetDawnGlow("大烧", models, model.Missing(), 0); g != "无" {
		t.Fatalf("sunset 口径应排除 ICON 离群、对齐 sunsetbot 不烧，实际 %q", g)
	}
}

// TestSunsetDawnGlow_GFSECMWFAgree GFS/ECMWF 一致大烧 → 大烧（两基准模型都支撑）。
func TestSunsetDawnGlow_GFSECMWFAgree(t *testing.T) {
	models := map[string]string{"gfs_seamless": "大烧", "ecmwf_ifs025": "大烧"}
	if g, _ := sunsetDawnGlow("无", models, model.Missing(), 0); g != "大烧" {
		t.Fatalf("GFS/ECMWF 一致大烧应判大烧，实际 %q", g)
	}
}

// TestSunsetDawnGlow_GFSHighECMWFNone GFS 大烧但 ECMWF 判无 → 封顶无（ECMWF 否决）。
func TestSunsetDawnGlow_GFSHighECMWFNone(t *testing.T) {
	models := map[string]string{"gfs_seamless": "大烧", "ecmwf_ifs025": "无"}
	if g, _ := sunsetDawnGlow("大烧", models, model.Missing(), 0); g != "无" {
		t.Fatalf("ECMWF 判无应封顶为无，实际 %q", g)
	}
}

// TestSunsetDawnGlow_SingleModelFallback 单模型（无 GFS/ECMWF 对照）退回主模型 + AOD。
func TestSunsetDawnGlow_SingleModelFallback(t *testing.T) {
	if g, _ := sunsetDawnGlow("中烧", map[string]string{}, model.Missing(), 0); g != "中烧" {
		t.Fatalf("单模型应退回主模型结论，实际 %q", g)
	}
}

// TestSunsetDawnGlow_GeoGate 几何光照门：评估时次太阳高度过低（仍在地球阴影）时
// 即便有云载体也封顶小烧，避免对粗分辨率数据误报大烧。
func TestSunsetDawnGlow_GeoGate(t *testing.T) {
	models := map[string]string{"gfs_seamless": "大烧", "ecmwf_ifs025": "大烧"}
	if g, _ := sunsetDawnGlow("大烧", models, model.Missing(), -12); g != "小烧" {
		t.Fatalf("几何门应封顶小烧，实际 %q", g)
	}
}

// TestBuildSunriseReport_GlowPolicySunset 锁死 --glow-policy sunset 接线：
// 同一份输入（ICON 大烧 / GFS 小烧 / ECMWF 无 / best 无），sunset 口径排除 ICON 离群 → 无，
// 且逐模型明细展示全部 4 个模型、把 GFS 标记为基准(主)、分歧描述含「分歧」。
func TestBuildSunriseReport_GlowPolicySunset(t *testing.T) {
	cfg := config.Default()
	night := mergeNight
	sunriseDate := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	resp := makeCloudSeaResp(t)

	models := map[string]string{
		"icon_seamless": "大烧",
		"gfs_seamless":  "小烧",
		"ecmwf_ifs025":  "无",
		"best_match":    "无",
	}
	res := BuildSunriseReport(mergeSite, resp, night, sunriseDate, cfg, 28800, 30,
		DawnGlowContext{PrimaryModel: "icon_seamless", Models: models, GlowPolicy: "sunset"})
	if res.DawnGlow != "无" {
		t.Fatalf("sunset 口径应排除 ICON 离群、对齐 sunsetbot 不烧，实际 %q", res.DawnGlow)
	}
	if len(res.DawnGlowModels) != 4 {
		t.Fatalf("DawnGlowModels 应有 4 条（全模型明细），实际 %d: %+v", len(res.DawnGlowModels), res.DawnGlowModels)
	}
	if res.DawnGlowDivergence == "" || !strings.Contains(res.DawnGlowDivergence, "分歧") {
		t.Errorf("sunset 多模型下应给出含「分歧」的描述，实际 %q", res.DawnGlowDivergence)
	}
	foundGFS := false
	for _, m := range res.DawnGlowModels {
		if m.Model == "gfs_seamless" && m.Primary {
			foundGFS = true
		}
	}
	if !foundGFS {
		t.Errorf("sunset 口径下 GFS 应标记为基准(主)：%+v", res.DawnGlowModels)
	}
}
