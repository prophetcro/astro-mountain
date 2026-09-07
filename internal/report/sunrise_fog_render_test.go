package report

import (
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func TestRenderSunriseFog_ShownWhenPresent(t *testing.T) {
	res := []SunriseSiteResult{{
		Site:           "佘山",
		SunriseTime:    time.Date(2026, 9, 6, 5, 37, 0, 0, time.UTC),
		ArriveBy:       time.Date(2026, 9, 6, 4, 7, 0, 0, time.UTC),
		Episodes:       nil,
		CloudSeaHours:  0,
		HasData:        true,
		SunriseDate:    "2026-09-06",
		DawnGlow:       "无",
		DawnGlowNote:   "低云量 60% 偏高，日出处被遮挡",
		FogPotential:   "中",
		FogNote:        "地面RH 96%、温露差 1.2℃、风速 2.1m/s、能见度 800m",
		Confidence:     "极低",
		ConfidenceNote: "预报窗口内未检出云海",
		Rating:         "🔴 该夜无云海、朝霞亦弱",
	}}
	meta := model.ReportMeta{GeneratedAt: "2026-09-03 23:00", Mode: "sunrise"}
	out := BuildSunriseMarkdownReport(res, meta, config.Default())

	want := "**近地雾可能**：中 — 地面RH 96%、温露差 1.2℃、风速 2.1m/s、能见度 800m"
	if !strings.Contains(out, want) {
		t.Fatalf("报告未渲染近地雾行，期望包含：\n%s\n\n实际报告：\n%s", want, out)
	}
}

func TestRenderSunriseFog_SkippedWhenNone(t *testing.T) {
	res := []SunriseSiteResult{{
		Site:           "佘山",
		SunriseTime:    time.Date(2026, 9, 6, 5, 37, 0, 0, time.UTC),
		ArriveBy:       time.Date(2026, 9, 6, 4, 7, 0, 0, time.UTC),
		Episodes:       nil,
		CloudSeaHours:  0,
		HasData:        true,
		SunriseDate:    "2026-09-06",
		DawnGlow:       "无",
		DawnGlowNote:   "低云量 60% 偏高",
		FogPotential:   "无",
		Confidence:     "极低",
		ConfidenceNote: "预报窗口内未检出云海",
		Rating:         "🔴 该夜无云海、朝霞亦弱",
	}}
	meta := model.ReportMeta{GeneratedAt: "2026-09-03 23:00", Mode: "sunrise"}
	out := BuildSunriseMarkdownReport(res, meta, config.Default())

	if strings.Contains(out, "近地雾可能") {
		t.Fatalf("FogPotential=无 时仍渲染了近地雾行：\n%s", out)
	}
}

func TestRenderSunriseFogPeriods_ShownWhenPresent(t *testing.T) {
	res := []SunriseSiteResult{{
		Site:           "太子尖",
		SunriseTime:    time.Date(2026, 9, 6, 5, 47, 0, 0, time.UTC),
		ArriveBy:       time.Date(2026, 9, 6, 4, 17, 0, 0, time.UTC),
		Episodes:       nil,
		CloudSeaHours:  0,
		HasData:        true,
		SunriseDate:    "2026-09-06",
		DawnGlow:       "无",
		DawnGlowNote:   "低云量 60% 偏高",
		FogPotential:   "强",
		FogNote:        "地面RH 90%、温露差 1.6℃、风速 1.2m/s",
		// 关键：云海时段为无，但辐射雾时段独立存在——两者必须分开成行。
		FogPeriods: []FogPeriod{
			{Start: time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC),
				End: time.Date(2026, 9, 6, 8, 0, 0, 0, time.UTC),
				PeakLevel: "强", PeakHour: time.Date(2026, 9, 6, 5, 0, 0, 0, time.UTC)},
		},
		Confidence:     "极低",
		ConfidenceNote: "预报窗口内未检出云海",
		Rating:         "🔴 该夜无云海、朝霞亦弱",
	}}
	meta := model.ReportMeta{GeneratedAt: "2026-09-03 23:00", Mode: "sunrise"}
	out := BuildSunriseMarkdownReport(res, meta, config.Default())

	// 辐射雾时段行必须出现，且独立于「近地雾可能」与「云海时段」。
	if !strings.Contains(out, "**辐射雾时段**：01:00–08:00 强（峰值 05:00）") {
		t.Fatalf("报告未渲染辐射雾时段行，期望包含「01:00–08:00 强（峰值 05:00）」：\n%s", out)
	}
	if !strings.Contains(out, "**近地雾可能**：强") {
		t.Errorf("近地雾可能行应仍在：\n%s", out)
	}
}

func TestRenderSunriseFogPeriods_SkippedWhenEmpty(t *testing.T) {
	res := []SunriseSiteResult{{
		Site:           "佘山",
		SunriseTime:    time.Date(2026, 9, 6, 5, 37, 0, 0, time.UTC),
		ArriveBy:       time.Date(2026, 9, 6, 4, 7, 0, 0, time.UTC),
		Episodes:       nil,
		CloudSeaHours:  0,
		HasData:        true,
		SunriseDate:    "2026-09-06",
		DawnGlow:       "无",
		FogPotential:   "无",
		Confidence:     "极低",
		ConfidenceNote: "预报窗口内未检出云海",
		Rating:         "🔴 该夜无云海、朝霞亦弱",
		// FogPeriods 缺省（nil）→ 不渲染辐射雾时段行。
	}}
	meta := model.ReportMeta{GeneratedAt: "2026-09-03 23:00", Mode: "sunrise"}
	out := BuildSunriseMarkdownReport(res, meta, config.Default())

	if strings.Contains(out, "辐射雾时段") {
		t.Fatalf("FogPeriods 为空时仍渲染了辐射雾时段行：\n%s", out)
	}
}
