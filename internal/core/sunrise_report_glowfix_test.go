package core

import (
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
	"github.com/prophetcro/astro-mountain/internal/report"
)

// TestApplySubmergedGlowFloor 验证淹没型朝霞物理地板：机位埋于云顶附近时，
// 云顶必被晨光染红，朝霞至少中烧——修正「只数中高云量、漏掉贴地云海」导致的低估。
func TestApplySubmergedGlowFloor(t *testing.T) {
	aodInvalid := model.OptFloat{Valid: false}
	aodExtreme := model.OptFloat{Valid: true, V: 1.2}

	cases := []struct {
		name      string
		tier      string
		submerged bool
		aod       model.OptFloat
		wantTier  string
	}{
		{"淹没型+无→中烧", "无", true, aodInvalid, "中烧"},
		{"淹没型+小烧→中烧", "小烧", true, aodInvalid, "中烧"},
		{"淹没型+中烧不变", "中烧", true, aodInvalid, "中烧"},
		{"淹没型+大烧不变", "大烧", true, aodInvalid, "大烧"},
		{"淹没型+AOD极端→尊重AOD", "无", true, aodExtreme, "无"},
		{"非淹没型+无→无", "无", false, aodInvalid, "无"},
		{"非淹没型+小烧→小烧", "小烧", false, aodInvalid, "小烧"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, note := applySubmergedGlowFloor(c.tier, "原note", c.submerged, c.aod)
			if got != c.wantTier {
				t.Fatalf("applySubmergedGlowFloor(tier=%q, submerged=%v) = %q，期望 %q（note=%s）",
					c.tier, c.submerged, got, c.wantTier, note)
			}
		})
	}
}

// TestSunriseObscuredWarning 验证「日出窗被云雾 100% 覆盖」警告：
// 覆盖近地辐射雾与淹没型云海两类，均意味着机位处被云雾包裹、日出看不见。
func TestSunriseObscuredWarning(t *testing.T) {
	sunrise := time.Date(2026, 9, 6, 5, 30, 0, 0, time.UTC)
	before, after := 45, 30

	t.Run("辐射雾覆盖窗口→警告", func(t *testing.T) {
		fps := []report.FogPeriod{
			{Start: sunrise.Add(-3 * time.Hour), End: sunrise.Add(2 * time.Hour)},
		}
		if got := sunriseObscuredWarning(fps, nil, sunrise, before, after, profile.FOG_STRONG); got == "" {
			t.Fatal("辐射雾全程覆盖窗口时应触发警告")
		} else {
			t.Logf("警告：%s", got)
		}
	})

	// 复现 9-06 牵牛岗漏报：雾 22:00–08:00 强覆盖日出窗，但模型把窗口段判轻雾使时段被截断，
	// 清晨段只从 05:10 才重新达「中」——单段都不「完整包含」窗口。旧逻辑零警告，新逻辑（触及即触发）应修。
	t.Run("辐射雾仅触及窗口(被截断未全程覆盖)→警告", func(t *testing.T) {
		fps := []report.FogPeriod{
			{Start: sunrise.Add(-20 * time.Minute), End: sunrise.Add(3 * time.Hour)}, // 05:10–08:30，只触及窗口
		}
		if got := sunriseObscuredWarning(fps, nil, sunrise, before, after, profile.FOG_STRONG); got == "" {
			t.Fatal("辐射雾触及日出窗（即便未全程覆盖）也应触发警告")
		} else {
			t.Logf("警告：%s", got)
		}
	})

	t.Run("辐射雾日出前已散→空", func(t *testing.T) {
		fps := []report.FogPeriod{
			{Start: sunrise.Add(-3 * time.Hour), End: sunrise.Add(-1 * time.Hour)},
		}
		if got := sunriseObscuredWarning(fps, nil, sunrise, before, after, profile.FOG_STRONG); got != "" {
			t.Fatalf("雾在日出前已散，不应触发；实际：%s", got)
		}
	})

	t.Run("淹没型云海覆盖窗口→警告", func(t *testing.T) {
		eps := []report.CloudSeaEpisode{
			{Start: sunrise.Add(-3 * time.Hour), End: sunrise.Add(1 * time.Hour), Submerged: true},
		}
		if got := sunriseObscuredWarning(nil, eps, sunrise, before, after, ""); got == "" {
			t.Fatal("淹没型云海全程覆盖窗口时应触发警告")
		} else {
			t.Logf("警告：%s", got)
		}
	})

	t.Run("脚下型云海(非淹没)覆盖窗口→空", func(t *testing.T) {
		eps := []report.CloudSeaEpisode{
			{Start: sunrise.Add(-3 * time.Hour), End: sunrise.Add(1 * time.Hour), Submerged: false},
		}
		if got := sunriseObscuredWarning(nil, eps, sunrise, before, after, ""); got != "" {
			t.Fatalf("脚下型非淹没不应触发；实际：%s", got)
		}
	})

	t.Run("无云雾→空", func(t *testing.T) {
		if got := sunriseObscuredWarning(nil, nil, sunrise, before, after, ""); got != "" {
			t.Fatalf("无云雾不应触发；实际：%s", got)
		}
	})
}
