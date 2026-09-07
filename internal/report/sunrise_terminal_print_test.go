package report

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// TestPrintSunriseSiteBlock_ShowsGlowWindow 回归 9-06 牵牛岗反馈：交互(终端)模式
// 以前只打印「朝霞强度」漏掉具体守候时刻，用户看不到「朝霞时刻」。
// 证明 printSunriseSiteBlock 现在会输出「朝霞窗口」行含起止时刻。
func TestPrintSunriseSiteBlock_ShowsGlowWindow(t *testing.T) {
	r := SunriseSiteResult{
		Site:         "牵牛岗",
		SunriseTime:  time.Date(2026, 9, 6, 5, 18, 0, 0, time.UTC),
		ArriveBy:     time.Date(2026, 9, 6, 4, 0, 0, 0, time.UTC),
		DawnGlow:     "中烧",
		GlowWindow:   GlowWindow{Start: time.Date(2026, 9, 6, 5, 39, 0, 0, time.UTC), End: time.Date(2026, 9, 6, 6, 24, 0, 0, time.UTC), Lit: true, DurationMin: 45},
		FogPotential: "强",
	}
	var buf bytes.Buffer
	printSunriseSiteBlock(&buf, []SunriseSiteResult{r})
	out := buf.String()
	if !strings.Contains(out, "朝霞窗口") {
		t.Fatalf("终端明细块未打印「朝霞窗口」行：\n%s", out)
	}
	if !strings.Contains(out, "05:39–06:24") {
		t.Fatalf("终端明细块未含朝霞窗口起止时刻：\n%s", out)
	}
}

// TestPrintSunriseSiteBlock_GlowWindowNoCarrier 无云载体时如实写「无」，而非给假时间窗。
func TestPrintSunriseSiteBlock_GlowWindowNoCarrier(t *testing.T) {
	r := SunriseSiteResult{
		Site:        "佘山",
		SunriseTime: time.Date(2026, 9, 6, 5, 37, 0, 0, time.UTC),
		ArriveBy:    time.Date(2026, 9, 6, 4, 7, 0, 0, time.UTC),
		DawnGlow:    "无",
		GlowWindow:  GlowWindow{Lit: false},
	}
	var buf bytes.Buffer
	printSunriseSiteBlock(&buf, []SunriseSiteResult{r})
	out := buf.String()
	if !strings.Contains(out, "朝霞窗口：无（无可染红云载体）") {
		t.Fatalf("无云载体时应如实写「无」：\n%s", out)
	}
}
