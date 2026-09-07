package report

import (
	"strings"
	"testing"
	"time"
)

// glowWindowAt 造一个指定起止的朝霞窗口，便于渲染层用例。
func glowWindowAt(start, end string, lit bool) GlowWindow {
	parse := func(s string) time.Time {
		t, err := time.Parse("15:04", s)
		if err != nil {
			panic(err)
		}
		return time.Date(2026, 9, 6, t.Hour(), t.Minute(), 0, 0, time.UTC)
	}
	return GlowWindow{Start: parse(start), End: parse(end), Lit: lit}
}

// TestGlowWindowLine_Normal 正常窗口渲染起止时刻与分钟数。
func TestGlowWindowLine_Normal(t *testing.T) {
	gw := glowWindowAt("05:32", "05:58", true)
	gw.DurationMin = 26
	got := glowWindowLine(gw)
	want := "**朝霞窗口**：05:32–05:58（26 分钟）"
	if got != want {
		t.Fatalf("渲染 = %q，期望 %q", got, want)
	}
}

// TestGlowWindowLine_Fleeting 窗口短于 15 分钟时追加「转瞬即逝」提示。
// 用户实地反馈「朝霞转瞬即逝」——短窗口必须提醒提前到位，否则等于没说。
func TestGlowWindowLine_Fleeting(t *testing.T) {
	gw := glowWindowAt("05:40", "05:49", true)
	gw.DurationMin = 9
	got := glowWindowLine(gw)
	if !strings.Contains(got, "05:40–05:49（9 分钟）") {
		t.Fatalf("短窗口未渲染起止与分钟数：%q", got)
	}
	if !strings.Contains(got, "⚠️ 转瞬即逝，建议提前 15 分钟到位守候") {
		t.Fatalf("短窗口未提示「转瞬即逝」：%q", got)
	}
}

// TestGlowWindowLine_NoCarrier 无云载体时如实写「无」，绝不给一个假时间窗。
func TestGlowWindowLine_NoCarrier(t *testing.T) {
	cases := []GlowWindow{
		{Lit: false, Reason: "无云载体"},                      // 判无载体
		glowWindowAt("05:32", "05:58", false),             // 有时刻但未判有效
		{Start: time.Time{}, End: time.Time{}, Lit: true}, // 时刻缺失
	}
	for i, gw := range cases {
		if got := glowWindowLine(gw); got != "**朝霞窗口**：无（无可染红云载体）" {
			t.Fatalf("case %d：无载体渲染 = %q，期望「无（无可染红云载体）」", i, got)
		}
	}
}

// TestGlowWindowRenderedInReport 端到端确认「朝霞窗口」行确实落在报告正文里，
// 且紧挨「朝霞强度」行——用户是在这一行下面找「几点守」的。
func TestGlowWindowRenderedInReport(t *testing.T) {
	r := SunriseSiteResult{
		Site:         "牵牛岗",
		SunriseTime:  time.Date(2026, 9, 6, 5, 47, 0, 0, time.UTC),
		ArriveBy:     time.Date(2026, 9, 6, 4, 17, 0, 0, time.UTC),
		DawnGlow:     "小烧",
		DawnGlowNote: "中高云量 22%，朝霞中等",
		GlowWindow:   glowWindowAt("05:39", "06:24", true),
	}
	r.GlowWindow.DurationMin = 45
	body := strings.Join(sunriseSiteDayBody(r), "\n")

	glowIdx := strings.Index(body, "**朝霞强度**")
	winIdx := strings.Index(body, "**朝霞窗口**")
	if glowIdx < 0 || winIdx < 0 {
		t.Fatalf("报告正文缺少朝霞窗口行：\n%s", body)
	}
	if winIdx < glowIdx {
		t.Fatalf("「朝霞窗口」应在「朝霞强度」之后：\n%s", body)
	}
	// 两行之间只允许夹一个空行（即紧挨着），避免被其他段落挤开。
	between := body[glowIdx+len("**朝霞强度**") : winIdx]
	if strings.Count(between, "\n") > 2 {
		t.Fatalf("「朝霞窗口」未紧挨「朝霞强度」：\n%s", body)
	}
	if !strings.Contains(body, "05:39–06:24（45 分钟）") {
		t.Fatalf("报告未渲染窗口起止与分钟数：\n%s", body)
	}
}
