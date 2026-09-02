package profile

import (
	"testing"

	"github.com/prophetcro/astro-mountain/internal/model"
)

// MaxGapAroundSite 是 2026-09 模型缺层降级方案的核心探测函数：
// 它返回「覆盖机位的上下相邻两层」之间的最大间距，>500m 即认为该模型的
// 垂直分辨率不足以可靠判定云海。
//
// 这个测试固定了三种典型形态：
//  1. ICON 11 层模式：盲区 ≤260m → 应返回 0 或 ≤260；
//  2. ECMWF/JMA 4 层模式：925↔850 间距 ≈ 732m → 应被检出 >500；
//  3. 机位恰好落在某层上：无盲区，应返回 0。
func TestMaxGapAroundSite(t *testing.T) {
	cases := []struct {
		name    string
		siteAlt float64
		levels  []Level
		wantMax float64
	}{
		{
			name:    "ICON 11 层模式牛草山",
			siteAlt: 1442,
			// 用 11 层 PressureLevels 的实测位势高（lat≈30, lon≈119）。
			// 机位 1442 落在 850hPa(1460) 与 875hPa(1231) 之间，盲区 ≈ 229m。
			levels: []Level{
				{Pressure: 1000, Height: 110, CC: model.Num(0)},
				{Pressure: 950, Height: 540, CC: model.Num(0)},
				{Pressure: 925, Height: 760, CC: model.Num(0)},
				{Pressure: 900, Height: 990, CC: model.Num(0)},
				{Pressure: 875, Height: 1231, CC: model.Num(0)},
				{Pressure: 850, Height: 1477, CC: model.Num(0)},
				{Pressure: 825, Height: 1737, CC: model.Num(0)},
				{Pressure: 800, Height: 1996, CC: model.Num(0)},
				{Pressure: 700, Height: 3123, CC: model.Num(0)},
			},
			wantMax: 246,
		},
		{
			name:    "ECMWF/JMA 4 层模式牛草山",
			siteAlt: 1442,
			// 只有 1000, 925, 850, 700；机位落在 925↔850 之间，盲区 ≈ 717m。
			levels: []Level{
				{Pressure: 1000, Height: 110, CC: model.Num(0)},
				{Pressure: 925, Height: 745, CC: model.Num(0)},
				{Pressure: 850, Height: 1477, CC: model.Num(0)},
				{Pressure: 700, Height: 3123, CC: model.Num(0)},
			},
			wantMax: 732,
		},
		{
			name:    "机位恰好落在层上（无上方层）",
			siteAlt: 1477,
			levels: []Level{
				{Pressure: 1000, Height: 110, CC: model.Num(0)},
				{Pressure: 850, Height: 1477, CC: model.Num(0)},
			},
			wantMax: 0,
		},
		{
			name:    "空廓线",
			siteAlt: 1442,
			levels:  nil,
			wantMax: 0,
		},
		{
			name:    "单层",
			siteAlt: 1442,
			levels:  []Level{{Pressure: 850, Height: 1477, CC: model.Num(0)}},
			wantMax: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MaxGapAroundSite(c.levels, c.siteAlt)
			// 容差：浮点比较，给 ±5m 余地。
			if got < c.wantMax-5 || got > c.wantMax+5 {
				t.Errorf("MaxGapAroundSite = %.1f，期望 ≈ %.1f", got, c.wantMax)
			}
		})
	}
}

// MaxGapAroundSite 的判定阈值（500m）边界值验证：
// 460m 应 < 500m 不触发告警，600m 应 >= 500m 触发告警。
func TestMaxGapAroundSiteThresholdBoundary(t *testing.T) {
	// 盲区 480m 的形态（机位落在 900↔850 之间，距 900 为 ~163m、距 850 为 ~317m）。
	levels := []Level{
		{Pressure: 900, Height: 983, CC: model.Num(0)},
		{Pressure: 850, Height: 1463, CC: model.Num(0)},
		{Pressure: 800, Height: 1996, CC: model.Num(0)},
	}
	// siteAlt=1146 落在 900↔850 之间，盲区 = 1463 - 983 = 480m < 500m。
	if gap := MaxGapAroundSite(levels, 1146); gap >= 500 {
		t.Errorf("盲区 %.0fm 应 < 500m 阈值（不触发告警），实际判为 %.0fm", gap, gap)
	}
	// siteAlt=1000 仍在 900↔850 之间，盲区还是 480m。
	if gap := MaxGapAroundSite(levels, 1000); gap >= 500 {
		t.Errorf("盲区 %.0fm 应 < 500m 阈值（不触发告警），实际判为 %.0fm", gap, gap)
	}

	// 盲区 732m 的形态（ECMWF/JMA 4 层典型）。
	ecmwfLevels := []Level{
		{Pressure: 1000, Height: 110, CC: model.Num(0)},
		{Pressure: 925, Height: 745, CC: model.Num(0)},
		{Pressure: 850, Height: 1477, CC: model.Num(0)},
		{Pressure: 700, Height: 3123, CC: model.Num(0)},
	}
	if gap := MaxGapAroundSite(ecmwfLevels, 1000); gap < 500 {
		t.Errorf("盲区 %.0fm 应 >= 500m 阈值（触发告警），实际判为 %.0fm", gap, gap)
	}
}
