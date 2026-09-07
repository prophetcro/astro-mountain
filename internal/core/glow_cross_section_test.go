package core

import (
	"math"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

func TestDestPoint(t *testing.T) {
	// 0km 必须原地不动，任何方位角都不该移动。
	for _, az := range []float64{0, 45, 90, 180, 270} {
		lat, lon := destPoint(30, 120, 0, az)
		if math.Abs(lat-30) > 1e-9 || math.Abs(lon-120) > 1e-9 {
			t.Errorf("dist=0 却移动了：az=%.0f → (%.6f,%.6f)，期望 (30,120)", az, lat, lon)
		}
	}
	// 正北 1° 纬度 ≈ 111.195km → 从赤道出发应到 lat≈1.0, lon≈0。
	lat, lon := destPoint(0, 0, 111.195, 0)
	if math.Abs(lat-1.0) > 1e-3 || math.Abs(lon-0.0) > 1e-3 {
		t.Errorf("正北 111.195km 应到 (1.0,0)，实际 (%.4f,%.4f)", lat, lon)
	}
	// 正东同样距离 → 应到 lat≈0, lon≈1.0。
	lat, lon = destPoint(0, 0, 111.195, 90)
	if math.Abs(lat-0.0) > 1e-3 || math.Abs(lon-1.0) > 1e-3 {
		t.Errorf("正东 111.195km 应到 (0,1.0)，实际 (%.4f,%.4f)", lat, lon)
	}
}

func TestRequiredElevDeg(t *testing.T) {
	if got := RequiredElevDeg(0, 100); got != 0 {
		t.Errorf("h=0 应返回 0，实际 %.4f", got)
	}
	if got := RequiredElevDeg(1000, 0); got != 0 {
		t.Errorf("d=0 应返回 0，实际 %.4f", got)
	}
	// atan(2000m / 800000m) = 0.1432°。
	want := math.Atan2(2000, 800000) * 180 / math.Pi
	if got := RequiredElevDeg(2000, 800); math.Abs(got-want) > 1e-6 {
		t.Errorf("RequiredElevDeg(2000,800) = %.6f，期望 %.6f", got, want)
	}
}

func TestAssessCrossSection_NoCloud(t *testing.T) {
	pts := []CrossSectionPoint{
		{DistKm: 0, HasCloud: false},
		{DistKm: 100, HasCloud: false},
		{DistKm: 800, HasCloud: false},
	}
	lit, _, reason := AssessCrossSection(pts)
	if lit {
		t.Errorf("全程无云却判为可达，应不可达")
	}
	if !strings.Contains(reason, "无有效云层") {
		t.Errorf("原因应包含「无有效云层」，实际 %q", reason)
	}
}

func TestAssessCrossSection_AODBlocks(t *testing.T) {
	// 云底 500m，AOD 0.15 → 等效光学地面抬升 750m → illum = -250m ≤ 0 → 被遮挡。
	pts := []CrossSectionPoint{
		{DistKm: 100, HasCloud: true, CloudBaseMSL: 500, MaxCC: 60, AOD: 0.15, HasAOD: true},
	}
	lit, _, reason := AssessCrossSection(pts)
	if lit {
		t.Errorf("AOD 把等效云底沉入光学地面却判可达，应不可达")
	}
	if !strings.Contains(reason, "气溶胶消光遮挡") {
		t.Errorf("原因应包含「气溶胶消光遮挡」，实际 %q", reason)
	}
}

func TestAssessCrossSection_Lit(t *testing.T) {
	// 云底 2000m，AOD 0.1 → 光学地面抬升 500m → illum=1500m>0，所需俯角极小 → 可达。
	pts := []CrossSectionPoint{
		{DistKm: 100, HasCloud: true, CloudBaseMSL: 2000, MaxCC: 50, AOD: 0.1, HasAOD: true},
		{DistKm: 800, HasCloud: false}, // 一个无云点不应影响「有可达点」的结论
	}
	lit, litPoints, reason := AssessCrossSection(pts)
	if !lit {
		t.Errorf("有云且 AOD 未遮挡却判不可达")
	}
	if litPoints != 1 {
		t.Errorf("可达点数应为 1，实际 %d", litPoints)
	}
	if !strings.Contains(reason, "可被曙/暮光照射") {
		t.Errorf("原因应包含「可被曙/暮光照射」，实际 %q", reason)
	}
}

func TestAssessCrossSection_Empty(t *testing.T) {
	// 空截面应安全返回不可达（与未启用等价），不 panic。
	lit, _, _ := AssessCrossSection(nil)
	if lit {
		t.Errorf("空截面不应判可达")
	}
}

func TestLowestCloudLevel_AlignedWithSunsetBar(t *testing.T) {
	// sunset 口径的「小烧」门是聚合中高云量 ≥5%。
	// 截面必须用同样宽松的 5% 门，绝不能比它更严（否则会把 sunset 给的
	// 「小烧」误压成「无」）。本测试锁死这一行为。
	levels := []profile.Level{
		{Pressure: 1000, Height: 100, CC: model.Num(2)}, // 低于 5% 门，不算云载体
		{Pressure: 925, Height: 760, CC: model.Num(6)},  // 首个 ≥5% 的层，应被选中
		{Pressure: 850, Height: 1460, CC: model.Num(40)},
	}
	l, ok := lowestCloudLevel(levels)
	if !ok {
		t.Fatalf("存在 ≥5%% 云量层却判无云载体")
	}
	if l.Height != 760 {
		t.Errorf("应取最低一个 ≥5%% 的层(海拔760m)，实际取到了海拔 %.0fm", l.Height)
	}

	// 全部 <5% → 无云载体。
	thin := []profile.Level{
		{Pressure: 1000, Height: 100, CC: model.Num(1)},
		{Pressure: 925, Height: 760, CC: model.Num(4)},
	}
	if _, ok := lowestCloudLevel(thin); ok {
		t.Errorf("全部 <5%% 却判有云载体，门槛不能严于 sunset 的 5%%")
	}

	// 云量缺测的层不算（不靠 0 兜底误判）。
	missing := []profile.Level{
		{Pressure: 925, Height: 760, CC: model.Missing()},
	}
	if _, ok := lowestCloudLevel(missing); ok {
		t.Errorf("云量缺测层不应算云载体")
	}
}
