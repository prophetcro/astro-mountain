package core

import (
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// cloudSeaThresh 返回一组用于测试的云海成因阈值（与 config.example.json 默认值一致）。
func cloudSeaThresh() config.Thresholds {
	return config.Thresholds{
		CloudSeaPrevNightPrecipMM: 1.0,
		CloudSeaCalmWindMS:        5.4,
		CloudSeaInversionBLHM:     800.0,
	}
}

func TestAssessCloudSeaCauseAllSatisfied(t *testing.T) {
	surface := model.Surface{
		WindSpeed10m:        model.Num(2.0), // ≤5.4 → 静风
		BoundaryLayerHeight: model.Num(450), // <800 → 逆温
	}
	th := cloudSeaThresh()
	c := assessCloudSeaCause(surface, model.REL_SEA_BELOW, 2.0, th) // 前晚降水2mm、头顶通透
	if !c.HasRain || !c.Cleared || !c.WindCalm || !c.InversionLikely {
		t.Fatalf("四要素应全部命中，实际：%+v", c)
	}
	if c.Score != 4 {
		t.Errorf("Score = %d，期望 4", c.Score)
	}
	if c.Confidence != "高" {
		t.Errorf("Confidence = %q，期望 高", c.Confidence)
	}
}

func TestAssessCloudSeaCausePartial(t *testing.T) {
	// 仅几何成立（头顶通透）+ 静风；无前晚降水、边界层高度缺测（不判逆温）。
	surface := model.Surface{
		WindSpeed10m:        model.Num(3.0),
		BoundaryLayerHeight: model.Missing(),
	}
	th := cloudSeaThresh()
	c := assessCloudSeaCause(surface, model.REL_SEA_BELOW, 0.0, th)
	if c.HasRain {
		t.Error("前晚无降水，HasRain 应为 false")
	}
	if !c.Cleared || !c.WindCalm {
		t.Errorf("头顶通透与静风应命中，实际：%+v", c)
	}
	if c.InversionLikely {
		t.Error("边界层高度缺测，InversionLikely 应为 false")
	}
	if c.Score != 2 {
		t.Errorf("Score = %d，期望 2", c.Score)
	}
	if c.Confidence != "中" {
		t.Errorf("Confidence = %q，期望 中", c.Confidence)
	}
}

func TestAssessCloudSeaCauseWindyNoInversion(t *testing.T) {
	// 风大（>3级）、无逆温、无前晚降水：仅几何成立 → 低置信。
	surface := model.Surface{
		WindSpeed10m:        model.Num(6.0),
		BoundaryLayerHeight: model.Num(1500), // ≥800 → 不判逆温
	}
	th := cloudSeaThresh()
	c := assessCloudSeaCause(surface, model.REL_SEA_BELOW, 0.0, th)
	if c.WindCalm || c.InversionLikely || c.HasRain {
		t.Fatalf("风大/无逆温/无降水均应不命中，实际：%+v", c)
	}
	if c.Score != 1 {
		t.Errorf("Score = %d，期望 1", c.Score)
	}
	if c.Confidence != "低" {
		t.Errorf("Confidence = %q，期望 低", c.Confidence)
	}
}

func TestPrevNightPrecipMMCountsPrevNightAndSameNightEarly(t *testing.T) {
	// 观测夜 ID 由 NightIDOf 决定（回拨 12h 取日期）：
	//   2026-08-11 23:00 与 2026-08-12 02:00 → 前晚 "2026-08-11"
	//   2026-08-12 22:00 与 2026-08-12 23:00 → 当前夜 "2026-08-12"
	resp := &api.Response{
		Times: []time.Time{
			time.Date(2026, 8, 11, 23, 0, 0, 0, time.UTC), // 前晚，降水 3mm
			time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC),  // 前晚凌晨，降水 1mm
			time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC), // 当前夜、早于拍摄时刻，降水 0.5mm
			time.Date(2026, 8, 12, 23, 0, 0, 0, time.UTC), // 当前拍摄时刻（idx=3），无降水
		},
		Series: map[string][]model.OptFloat{
			"precipitation": {
				model.Num(3.0), model.Num(1.0), model.Num(0.5), model.Num(0.0),
			},
		},
	}
	cfg := config.Config{
		Window: config.WindowConfig{NightStartHour: 22, NightEndHour: 6},
		Thresh: cloudSeaThresh(),
	}
	got := prevNightPrecipMM(resp, 3, cfg)
	want := 3.0 + 1.0 + 0.5 // 前晚 4mm + 同夜早于拍摄 0.5mm
	if got != want {
		t.Errorf("prevNightPrecipMM = %.1f，期望 %.1f", got, want)
	}
}

func TestPrevNightPrecipMMSameNightFallback(t *testing.T) {
	// 数据窗口只含单个观测夜（无前晚）：退化为「入夜以来到当前时刻」累计。
	resp := &api.Response{
		Times: []time.Time{
			time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC), // 当前夜，降水 0.5mm
			time.Date(2026, 8, 12, 23, 0, 0, 0, time.UTC), // 当前夜，降水 1.5mm
			time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC),  // 拍摄时刻（idx=2），无降水
		},
		Series: map[string][]model.OptFloat{
			"precipitation": {
				model.Num(0.5), model.Num(1.5), model.Num(0.0),
			},
		},
	}
	cfg := config.Config{
		Window: config.WindowConfig{NightStartHour: 22, NightEndHour: 6},
		Thresh: cloudSeaThresh(),
	}
	got := prevNightPrecipMM(resp, 2, cfg)
	want := 0.5 + 1.5
	if got != want {
		t.Errorf("prevNightPrecipMM(单夜退化) = %.1f，期望 %.1f", got, want)
	}
}

func TestCloudSeaConfidenceBoundaries(t *testing.T) {
	cases := []struct {
		score int
		want  string
	}{
		{0, "低"}, {1, "低"}, {2, "中"}, {3, "中"}, {4, "高"},
	}
	for _, c := range cases {
		if got := cloudSeaConfidence(c.score); got != c.want {
			t.Errorf("cloudSeaConfidence(%d) = %q，期望 %q", c.score, got, c.want)
		}
	}
}
