package core

import (
	"math"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/astro"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// 牵牛岗（用户 2026-09-06 凌晨实拍反馈「朝霞转瞬即逝」的点位）。
var glowSite = model.Site{Name: "牵牛岗", Lat: 30.026, Lon: 119.007, Alt: 1489.9}

// glowSunrise 取 2026-09-06 的当地日出时刻（UTC+8 墙钟），与生产口径一致。
func glowSunrise(t *testing.T) time.Time {
	t.Helper()
	loc := time.FixedZone("local", 8*3600)
	sr, ok := astro.SunriseTime(glowSite.Lat, glowSite.Lon, 8*3600,
		time.Date(2026, 9, 6, 0, 0, 0, 0, loc))
	if !ok {
		t.Fatal("牵牛岗 2026-09-06 未算出日出时刻")
	}
	return sr
}

// TestComputeGlowWindow_RealSiteQianniugang 用真实坐标（30.026,119.007 / 1489.9m）
// 与真实日期（2026-09-06）验证：淹没型（云顶高于机位）场景下窗口起点早于日出，
// 且时长落在 10~60 分钟的合理区间。
func TestComputeGlowWindow_RealSiteQianniugang(t *testing.T) {
	sunrise := glowSunrise(t)
	// 中高云云顶 4500m：比机位高约 3000m，云顶先见光 → 窗口起点早于日出。
	top := 4500.0
	gw := ComputeGlowWindow(glowSite, sunrise, top, true)

	if !gw.Lit {
		t.Fatalf("有云载体时应算出窗口，实际 Lit=false（reason=%s）", gw.Reason)
	}
	if !gw.Start.Before(sunrise) {
		t.Fatalf("云顶高于机位时窗口起点应早于日出：start=%s sunrise=%s",
			gw.Start.Format("15:04:05"), sunrise.Format("15:04:05"))
	}
	if !gw.End.After(sunrise) {
		t.Fatalf("窗口终点应晚于日出：end=%s sunrise=%s",
			gw.End.Format("15:04:05"), sunrise.Format("15:04:05"))
	}
	if gw.DurationMin < 10 || gw.DurationMin > 60 {
		t.Fatalf("正常山地场景窗口时长应落在 10~60 分钟，实际 %d 分钟（%s–%s）",
			gw.DurationMin, gw.Start.Format("15:04"), gw.End.Format("15:04"))
	}
	if d := int(math.Round(gw.End.Sub(gw.Start).Minutes())); d != gw.DurationMin {
		t.Fatalf("DurationMin(%d) 与 End-Start(%d) 不一致", gw.DurationMin, d)
	}
	t.Logf("牵牛岗 %s：日出 %s，云顶 %.0fm → 窗口 %s–%s（%d 分钟）",
		sunrise.Format("2006-01-02"), sunrise.Format("15:04:05"), top,
		gw.Start.Format("15:04"), gw.End.Format("15:04"), gw.DurationMin)
}

// TestComputeGlowWindow_NoCarrier 无云载体 → 窗口为空（不给假时间窗）。
func TestComputeGlowWindow_NoCarrier(t *testing.T) {
	sunrise := glowSunrise(t)
	gw := ComputeGlowWindow(glowSite, sunrise, 4500, false)
	if gw.Lit {
		t.Fatalf("无云载体时 Lit 应为 false，实际 true（%s–%s）",
			gw.Start.Format("15:04"), gw.End.Format("15:04"))
	}
	if gw.DurationMin != 0 {
		t.Fatalf("无云载体时 DurationMin 应为 0，实际 %d", gw.DurationMin)
	}
	if gw.Reason == "" {
		t.Fatal("无云载体时应给出原因（供报告渲染）")
	}
}

// TestComputeGlowWindow_EndAtFadeElevation 窗口终点对应的太阳高度角应等于消退角 8°。
func TestComputeGlowWindow_EndAtFadeElevation(t *testing.T) {
	sunrise := glowSunrise(t)
	gw := ComputeGlowWindow(glowSite, sunrise, 4500, true)
	if !gw.Lit {
		t.Fatalf("前置失败：应算出窗口（reason=%s）", gw.Reason)
	}
	got := sunAltAt(gw.End, glowSite.Lat, glowSite.Lon)
	if math.Abs(got-glowFadeElevDeg) > 0.05 {
		t.Fatalf("窗口终点太阳高度角 = %.3f°，期望 %.1f°（容差 0.05°）",
			got, glowFadeElevDeg)
	}
}

// TestComputeGlowWindow_StartElevationMatchesDip 窗口起点的太阳高度角应等于 −dip(H)，
// 且云顶越高（H 越大）起点越早——这正是「云顶比地面先见光」的物理。
func TestComputeGlowWindow_StartElevationMatchesDip(t *testing.T) {
	sunrise := glowSunrise(t)
	cases := []struct {
		name string
		top  float64
	}{
		{"云顶略高于机位", 2000.0},
		{"云顶明显高于机位", 4500.0},
		{"云顶远高于机位", 8000.0},
	}
	prevStart := time.Now().Add(24 * time.Hour) // 哨兵：确保逐个提前
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gw := ComputeGlowWindow(glowSite, sunrise, c.top, true)
			if !gw.Lit {
				t.Fatalf("前置失败：应算出窗口（reason=%s）", gw.Reason)
			}
			h := c.top - glowSite.Alt
			want := -horizonDipDeg(h)
			got := sunAltAt(gw.Start, glowSite.Lat, glowSite.Lon)
			if math.Abs(got-want) > 0.05 {
				t.Fatalf("窗口起点太阳高度角 = %.3f°，期望 %.3f°（−dip(%.0fm)）", got, want, h)
			}
			if !gw.Start.Before(prevStart) {
				t.Fatalf("云顶越高窗口起点应越早：本次 %s 未早于上一次 %s",
					gw.Start.Format("15:04:05"), prevStart.Format("15:04:05"))
			}
			prevStart = gw.Start
		})
	}
}

// TestComputeGlowWindow_SubmergedAndBeneathSharePhysics 淹没型（云顶在机位上方）
// 与脚下型（云顶在机位下方）共用同一套 dip 物理：前者窗口起点早于日出，
// 后者晚于日出，且提前/滞后量随高度差单调增大。
func TestComputeGlowWindow_SubmergedAndBeneathSharePhysics(t *testing.T) {
	sunrise := glowSunrise(t)

	above := ComputeGlowWindow(glowSite, sunrise, glowSite.Alt+1500, true)
	if !above.Lit || !above.Start.Before(sunrise) {
		t.Fatalf("淹没型（云顶高于机位 1500m）窗口起点应早于日出，实际 start=%s sunrise=%s lit=%v",
			above.Start.Format("15:04:05"), sunrise.Format("15:04:05"), above.Lit)
	}
	below := ComputeGlowWindow(glowSite, sunrise, glowSite.Alt-300, true)
	if !below.Lit || !below.Start.After(sunrise) {
		t.Fatalf("脚下型（云顶低于机位 300m）窗口起点应晚于日出，实际 start=%s sunrise=%s lit=%v",
			below.Start.Format("15:04:05"), sunrise.Format("15:04:05"), below.Lit)
	}
	if !below.Start.After(above.Start) {
		t.Fatalf("脚下型窗口起点(%s) 应晚于淹没型(%s)——同一套 dip 物理下符号相反",
			below.Start.Format("15:04:05"), above.Start.Format("15:04:05"))
	}
	t.Logf("淹没型(云顶+1500m) 起点 %s；脚下型(云顶-300m) 起点 %s；日出 %s",
		above.Start.Format("15:04:05"), below.Start.Format("15:04:05"), sunrise.Format("15:04:05"))
}

// TestHorizonDipDeg 校验地平俯角公式：dip(H)=acos(R/(R+H))，
// 小 H 时 ≈ sqrt(2H/R) 弧度；H=0 时为 0；H 取负时符号对称翻转。
func TestHorizonDipDeg(t *testing.T) {
	if got := horizonDipDeg(0); got != 0 {
		t.Fatalf("dip(0) = %v，期望 0", got)
	}
	const h = 2000.0
	want := math.Acos(earthRadiusM/(earthRadiusM+h)) * 180 / math.Pi
	if got := horizonDipDeg(h); math.Abs(got-want) > 1e-9 {
		t.Fatalf("dip(%.0f) = %.6f°，期望 %.6f°", h, got, want)
	}
	// 小 H 近似：sqrt(2H/R) 弧度→度，相对误差应在千分量级。
	approx := math.Sqrt(2*h/earthRadiusM) * 180 / math.Pi
	if got := horizonDipDeg(h); math.Abs(got-approx)/approx > 0.01 {
		t.Fatalf("dip(%.0f)=%.4f° 与近似值 %.4f° 相差过大", h, got, approx)
	}
	if got := horizonDipDeg(-h); math.Abs(got-(-want)) > 1e-9 {
		t.Fatalf("dip(-%.0f) = %.6f°，期望 %.6f°（对称翻转）", h, got, -want)
	}
}

// TestComputeGlowWindow_DurationReasonable 多个真实点位/高度的窗口都该落在 10~60 分钟，
// 防止算出负数或几小时的错误值（例如 dip 符号写反、扫描步长算错）。
func TestComputeGlowWindow_DurationReasonable(t *testing.T) {
	loc := time.FixedZone("local", 8*3600)
	sites := []model.Site{
		{Name: "牵牛岗", Lat: 30.026, Lon: 119.007, Alt: 1489.9},
		{Name: "东白山", Lat: 29.4785, Lon: 120.4448, Alt: 1194.6},
		{Name: "金华山·北山", Lat: 29.2350, Lon: 119.6610, Alt: 1311.1},
	}
	// 涵盖脚下型（云顶低于机位）到典型中高云高度。
	tops := []float64{1000.0, 2500.0, 4500.0, 7000.0}
	for _, s := range sites {
		sr, ok := astro.SunriseTime(s.Lat, s.Lon, 8*3600, time.Date(2026, 9, 6, 0, 0, 0, 0, loc))
		if !ok {
			t.Fatalf("%s 未算出日出时刻", s.Name)
		}
		for _, top := range tops {
			gw := ComputeGlowWindow(s, sr, top, true)
			if !gw.Lit {
				t.Fatalf("%s 云顶%.0fm：应算出窗口（reason=%s）", s.Name, top, gw.Reason)
			}
			if gw.DurationMin < 10 || gw.DurationMin > 60 {
				t.Fatalf("%s 云顶%.0fm：窗口 %d 分钟（%s–%s）超出 10~60 合理区间",
					s.Name, top, gw.DurationMin,
					gw.Start.Format("15:04"), gw.End.Format("15:04"))
			}
		}
	}
}
