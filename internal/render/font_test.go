package render

import (
	"math"
	"strings"
	"testing"
)

func requireFont(t *testing.T) {
	t.Helper()
	if _, err := ResolveFontPath(""); err != nil {
		t.Skipf("本机无可用中文字体，跳过（这是环境问题，非代码缺陷）：%v", err)
	}
}

func referenceFontLoaded() bool {
	return strings.Contains(FontPath(), "Hiragino Sans GB")
}

func TestPyRoundMatchesPython(t *testing.T) {
	cases := []struct {
		in   float64
		want int
		note string
	}{

		{0.5, 0, "0.5 → 0（偶）"},
		{1.5, 2, "1.5 → 2（偶）"},
		{2.5, 2, "2.5 → 2（偶）"},
		{3.5, 4, "3.5 → 4（偶）"},
		{4.5, 4, "4.5 → 4（偶）"},
		{-0.5, 0, "-0.5 → 0"},
		{-1.5, -2, "-1.5 → -2"},
		{-2.5, -2, "-2.5 → -2"},

		{2.4, 2, ""},
		{2.6, 3, ""},
		{0.0, 0, ""},
		{-2.4, -2, ""},
		{-2.6, -3, ""},

		{BaseTitle * 1.0, 66, "title@1.0"},
		{BaseTitle * 0.9, 59, "title@0.9"},
		{BaseTitle * 0.81, 53, "title@0.81 = 53.46 → 53"},
		{BaseTitle * 0.729, 48, "title@0.729 = 48.114 → 48"},
		{BaseBody * 1.0, 38, "body@1.0"},
		{BaseBody * 0.9, 34, "body@0.9 = 34.2 → 34"},
		{BaseBody * 0.81, 31, "body@0.81 = 30.78 → 31"},
		{BaseTable * 0.9, 29, "table@0.9 = 28.8 → 29"},
		{BaseTable * HardFloorScale, 13, "table@0.4 = 12.8 → 13"},
	}
	for _, c := range cases {
		got := pyRound(c.in)
		if got != c.want {
			t.Errorf("pyRound(%v) = %d, want %d  %s", c.in, got, c.want, c.note)
		}
	}
}

func TestPyRoundDiffersFromMathRound(t *testing.T) {
	diverged := false
	for _, v := range []float64{0.5, 2.5, 4.5} {
		if pyRound(v) != int(math.Round(v)) {
			diverged = true
			break
		}
	}
	if !diverged {
		t.Error("pyRound 与 math.Round 行为一致——banker's rounding 未生效，" +
			"缩放档位将与 Python 版漂移")
	}
}

func TestPyRoundHandlesNonFinite(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got := pyRound(v); got != 0 {
			t.Errorf("pyRound(%v) = %d, want 0（非有限值应安全兜底）", v, got)
		}
	}
}

func TestResolveFontPathOverrideMissing(t *testing.T) {
	_, err := ResolveFontPath("/definitely/not/a/font.ttf")
	if err == nil {
		t.Fatal("指定不存在的字体应返回错误，不能静默降级")
	}
	msg := err.Error()
	if !strings.Contains(msg, "douyin.font_path") {
		t.Errorf("错误信息应点名配置项 douyin.font_path，实际：%v", err)
	}
	if !strings.Contains(msg, "/definitely/not/a/font.ttf") {
		t.Errorf("错误信息应包含尝试过的路径，实际：%v", err)
	}
}

func TestResolveFontPathReportsAllCandidates(t *testing.T) {
	if _, err := ResolveFontPath(""); err == nil {
		t.Skip("本机有可用字体，无法验证全失败路径的错误信息")
	} else {
		if !strings.Contains(err.Error(), "configs/config.json") {
			t.Errorf("错误信息应引导用户配置字体，实际：%v", err)
		}
	}
}

func TestFontCandidatesExcludeSTHeiti(t *testing.T) {
	for _, p := range FontCandidates {
		if strings.Contains(p, "STHeiti") {
			t.Errorf("候选表不应包含 STHeiti（Apple 专有 cmap，x/image 不支持）：%s", p)
		}
	}
}

func TestFontCandidatesCoverAllPlatforms(t *testing.T) {
	var mac, win, linux bool
	for _, p := range FontCandidates {
		switch {
		case strings.HasPrefix(p, "/System/") || strings.HasPrefix(p, "/Library/"):
			mac = true
		case strings.HasPrefix(p, "C:/Windows/"):
			win = true
		case strings.HasPrefix(p, "/usr/share/fonts/"):
			linux = true
		}
	}
	if !mac || !win || !linux {
		t.Errorf("候选表平台覆盖不全：mac=%v win=%v linux=%v", mac, win, linux)
	}
}

func TestLoadFontCaching(t *testing.T) {
	requireFont(t)

	a, err := LoadFont(38, false)
	if err != nil {
		t.Fatalf("LoadFont: %v", err)
	}
	b, err := LoadFont(38, false)
	if err != nil {
		t.Fatalf("LoadFont: %v", err)
	}
	if a != b {
		t.Error("同字号同字重应命中缓存返回同一实例")
	}
	if c, _ := LoadFont(38, true); c == a {
		t.Error("粗体与常规应是不同实例")
	}
	if a.Size != 38 {
		t.Errorf("Font.Size = %d, want 38", a.Size)
	}
}

func TestLoadFontClampsTinySize(t *testing.T) {
	requireFont(t)

	for _, size := range []int{0, -5} {
		f, err := LoadFont(size, false)
		if err != nil {
			t.Fatalf("LoadFont(%d): %v", size, err)
		}
		if f.Size != 1 {
			t.Errorf("LoadFont(%d).Size = %d, want 1", size, f.Size)
		}
	}
}

func TestLoadFontAllScaleTiers(t *testing.T) {
	requireFont(t)

	scale := 1.0
	tiers := 0
	for scale >= HardFloorScale {
		for _, base := range []int{BaseTitle, BaseSubhead, BaseBody, BaseTable, BaseSmall} {
			if _, err := LoadFont(pyRound(float64(base)*scale), false); err != nil {
				t.Fatalf("scale=%.4f base=%d 建 face 失败：%v", scale, base, err)
			}
		}
		tiers++
		scale *= 0.9
	}
	if tiers < 9 {
		t.Errorf("覆盖档位数 = %d, want >= 9（1.0 逐档 ×0.9 到 0.4）", tiers)
	}
}

func TestFontWidthAdditivity(t *testing.T) {
	requireFont(t)

	f, err := LoadFont(40, false)
	if err != nil {
		t.Fatalf("LoadFont: %v", err)
	}
	const sample = "山地星野 低云海拔评估 2026-08-13"

	var sum float64
	for _, r := range sample {
		sum += f.Advance(r)
	}
	whole := f.Measure(sample)

	if diff := math.Abs(sum - whole); diff > 0.01 {
		t.Errorf("宽度可加性不成立：逐字符累加 %.4f vs 整串 %.4f（差 %.4f）"+
			"—— WrapText 的增量折行会失准", sum, whole, diff)
	}
}

func TestFontLineHeightFormula(t *testing.T) {
	requireFont(t)

	for _, size := range []int{60, 44, 38, 32, 26, 16} {
		f, err := LoadFont(size, false)
		if err != nil {
			t.Fatalf("LoadFont(%d): %v", size, err)
		}
		want := int(float64(size)*1.45) + 2
		if got := f.LineHeight(); got != want {
			t.Errorf("LineHeight(%dpx) = %d, want %d", size, got, want)
		}
	}
}

func TestWrapTextNeverExceedsWidth(t *testing.T) {
	requireFont(t)

	f, err := LoadFont(38, false)
	if err != nil {
		t.Fatalf("LoadFont: %v", err)
	}
	const maxW = 400.0
	samples := []string{
		"低云海拔评估明细 · 2026-08-12 夜（1/3）",
		"云底/云顶AGL 均为相对机位高度（正=在机位之上，负=在机位之下）",
		"a very long ascii sentence that must wrap at word boundaries not mid word",
		"混排 mixed CJK and ASCII 文本 with numbers 20260812 交替出现",
	}
	for _, s := range samples {
		for i, line := range WrapText(s, f, maxW) {

			if len([]rune(line)) <= 1 {
				continue
			}
			if w := f.Measure(line); w > maxW+0.5 {
				t.Errorf("折行超宽：%q 第 %d 行 %q = %.1fpx > %.1f", s, i, line, w, maxW)
			}
		}
	}
}

func TestWrapTextLosesNoContent(t *testing.T) {
	requireFont(t)

	f, err := LoadFont(32, false)
	if err != nil {
		t.Fatalf("LoadFont: %v", err)
	}
	const src = "低云海拔评估明细 · 2026-08-12 夜（1/3）核心窗口 23:00-05:00"
	joined := strings.Join(WrapText(src, f, 260), "")

	strip := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == ' ' {
				return -1
			}
			return r
		}, s)
	}
	if strip(joined) != strip(src) {
		t.Errorf("折行丢字：\n  原文 %q\n  拼回 %q", strip(src), strip(joined))
	}
}

func TestWrapTextEdgeCases(t *testing.T) {
	requireFont(t)

	f, _ := LoadFont(38, false)

	if got := WrapText("", f, 400); len(got) != 1 || got[0] != "" {
		t.Errorf(`WrapText("") = %q, want [""]`, got)
	}
	if got := WrapText("任意文本", f, 0); len(got) != 1 || got[0] != "任意文本" {
		t.Errorf("maxWidth<=0 应原样返回，实际 %q", got)
	}
	if got := WrapText("第一行\n第二行", f, 9999); len(got) != 2 {
		t.Errorf("显式换行未生效：%q", got)
	}
}
