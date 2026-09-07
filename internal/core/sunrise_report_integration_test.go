package core

import (
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
)

// makeSubmergedResp 构造「淹没型云海」合成响应，复用 cloudsea_submerged_test.go 里
// 经过验证的 subBuildResp 造数器：1000→850hPa 全部有云（合并成一层 300→1477m 的厚云），
// 机位(1442m)嵌在云层里、云顶仅略高于机位(1477−1442=35m)。这正是 2026-09 修复锁定的
// 「淹没型云海」标准形态：脚下厚云海 + 机位在云顶附近（REL_IN_CLOUD + 脚下够厚 + 头顶够薄）。
//
// 地面低云 60%（≥40% 触发云海「有」），中高云量压低(mid=20/high=10)——
// assessDawnGlow 因低云量≥60% 直接判「无」，正是 #2 要纠正的「只数云量、漏掉贴身云海」场景。
// 时间轴沿用 makeCloudSeaResp 的 23:00–07:00 当地整点，完整覆盖日出窗。
func makeSubmergedResp(t *testing.T) *api.Response {
	t.Helper()
	cloud := subFlat(70)
	// subBuildResp 基于 makeCloudSeaResp 重建所有气压层：未列出的层 CC/RH 缺测被丢弃，
	// 列出的层写 70% 云量 + subGH 位势高。1000→850 连续有云 → 单层淹没机位。
	// 与 TestCloudSeaSubmergedFlagIsSet 的「淹没机位(1000→850全有云)」场景完全一致，
	// 该场景已被锁死为 Submerged=true，可保证本集成测试拿到真正的淹没型时段。
	resp := subBuildResp(t, map[int][]float64{
		1000: cloud, 975: cloud, 950: cloud, 925: cloud,
		900: cloud, 875: cloud, 850: subFlat(60), 825: subFlat(0), 800: subFlat(0),
	})
	return resp
}

// TestBuildSunriseReport_SubmergedGlowBoostAndObscured 端到端验证 #2/#3 接线：
// 淹没型云海全程覆盖日出窗、近地中高云量低（assessDawnGlow 判无）的合成场景，
// 验证 BuildSunriseReport 会：① 把朝霞地板抬到中烧（#2）；② 触发「日出窗被云雾覆盖」警告（#3）。
func TestBuildSunriseReport_SubmergedGlowBoostAndObscured(t *testing.T) {
	cfg := config.Default()
	sunriseDate := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	resp := makeSubmergedResp(t)

	// 单模型（DawnGlowContext{}）：不引入跨模型共识/降级，纯粹验证淹没型物理地板。
	res := BuildSunriseReport(mergeSite, resp, mergeNight, sunriseDate, cfg, 28800, 30, DawnGlowContext{})

	if !strings.Contains(res.CloudSeaForm, "淹没") {
		t.Fatalf("期望云海形态为淹没型，实际 %q（episodes=%d）", res.CloudSeaForm, len(res.Episodes))
	}

	// #2：淹没型 + 中高云量低（assessDawnGlow 判无）→ 物理地板抬到中烧。
	if res.DawnGlow != "中烧" {
		t.Errorf("#2 淹没型朝霞应至少中烧，实际 %q（note=%s）", res.DawnGlow, res.DawnGlowNote)
	} else {
		t.Logf("#2 淹没型朝霞已抬到中烧：%s", res.DawnGlowNote)
	}

	// #3：淹没型云海覆盖日出窗 → 触发警告。
	if res.ObscuredWarning == "" {
		t.Error("#3 淹没型云海覆盖日出窗应触发「日出窗被云雾覆盖」警告")
	} else {
		t.Logf("#3 警告：%s", res.ObscuredWarning)
	}
}
