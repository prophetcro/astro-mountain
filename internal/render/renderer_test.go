package render

import (
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const sampleReport = "testdata/sample_report.md"

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

func TestRenderDouyinNotFound(t *testing.T) {
	outputs, err := RenderDouyin("/this/path/does/not/exist.md", Options{
		OutDir: t.TempDir(),
		Logger: discardLogger(),
	})
	if err == nil {
		t.Fatal("报告不存在时应返回错误")
	}
	if outputs != nil {
		t.Errorf("失败时 outputs 应为 nil，实际 %v", outputs)
	}
	if !strings.Contains(err.Error(), "读取失败") {
		t.Errorf("错误信息应说明是读取失败，实际：%v", err)
	}
}

func TestRenderDouyinFontInvalid(t *testing.T) {
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "mini.md")
	mustWrite(t, mdPath, "# 标题\n\n## 1.1 点位列表\n\n正文\n")

	outputs, err := RenderDouyin(mdPath, Options{
		OutDir:   tmp,
		FontPath: "/definitely/not/a/font.ttf",
		Logger:   discardLogger(),
	})
	if err == nil {
		t.Fatal("字体不可用时应返回错误，不能静默降级")
	}
	if outputs != nil {
		t.Errorf("失败时 outputs 应为 nil，实际 %v", outputs)
	}
	if !strings.Contains(err.Error(), "douyin.font_path") {
		t.Errorf("错误信息应点名配置项，实际：%v", err)
	}
}

func TestRenderDouyinNoMatchingSection(t *testing.T) {
	requireFont(t)

	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "empty.md")
	mustWrite(t, mdPath, "# 标题\n\n## 完全无关的小节\n\n正文\n")

	if _, err := RenderDouyin(mdPath, Options{
		OutDir: tmp,
		Logger: discardLogger(),
	}); err == nil {
		t.Fatal("没有任何小节命中时应返回错误")
	}
}

func TestRenderDouyinNoHeadings(t *testing.T) {
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "flat.md")
	mustWrite(t, mdPath, "只有正文，没有任何二级标题。\n")

	if _, err := RenderDouyin(mdPath, Options{
		OutDir:   tmp,
		FontPath: "/definitely/not/a/font.ttf",
		Logger:   discardLogger(),
	}); err == nil {
		t.Fatal("无标题报告应返回错误")
	}
}

func TestRenderDouyinRecoversFromPanic(t *testing.T) {
	tmp := t.TempDir()
	mdPath := filepath.Join(tmp, "mini.md")
	mustWrite(t, mdPath, "# 标题\n\n## 1.1 点位列表\n\n正文\n")

	ResetFontCache()
	func() {
		defer func() {
			if rec := recover(); rec == nil {
				t.Log("mustLoadFont 未 panic（本机字体已就绪），跳过 panic 触发验证")
			}
		}()
		_ = NewStyle(1.0)
	}()

	ResetFontCache()
	defer ResetFontCache()
	_, err := RenderDouyin(mdPath, Options{OutDir: tmp, Logger: discardLogger()})
	_ = err
}

func TestOptionsSectionKeywordsDefault(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", strings.Split(DefaultSections, ",")},
		{"  ", strings.Split(DefaultSections, ",")},
		{"点位列表,核心窗口", []string{"点位列表", "核心窗口"}},
		{" 点位列表 , , 核心窗口 ", []string{"点位列表", "核心窗口"}},
	}
	for _, c := range cases {
		got := Options{Sections: c.in}.sectionKeywords()
		if len(got) != len(c.want) {
			t.Errorf("sectionKeywords(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("sectionKeywords(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestDefaultSectionsMatchesPython(t *testing.T) {
	const python = "点位列表,天文条件,核心窗口,低云海拔评估明细"
	if DefaultSections != python {
		t.Errorf("DefaultSections = %q, want %q（须与 gen_douyin.py 一致）",
			DefaultSections, python)
	}
}

func TestRenderDouyinSampleReport(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过端到端出图")
	}
	requireFont(t)

	outDir := t.TempDir()
	results, err := RenderDouyinDetailed(sampleReport, Options{
		OutDir: outDir,
		Logger: discardLogger(),
	})
	if err != nil {
		t.Fatalf("RenderDouyinDetailed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("未产出任何图片")
	}

	for _, r := range results {
		if r.Width != CanvasW || r.Height != CanvasH {
			t.Errorf("%s 尺寸 = %dx%d, want %dx%d",
				filepath.Base(r.Path), r.Width, r.Height, CanvasW, CanvasH)
		}
		assertValidPNG(t, r.Path)
	}

	names := make([]string, 0, len(results))
	for _, r := range results {
		names = append(names, filepath.Base(r.Path))
	}
	sort.Strings(names)

	wantAlways := []string{
		"sample_report_cloud_detail_2026-08-12_1.png",
		"sample_report_cloud_detail_2026-08-12_2.png",
		"sample_report_cloud_detail_2026-08-13_1.png",
		"sample_report_cloud_detail_2026-08-13_2.png",
		"sample_report_sites.png",
		"sample_report_astro.png",
	}
	for _, want := range wantAlways {
		if !contains(names, want) {
			t.Errorf("缺少必现文件 %s\n实际产出：%v", want, names)
		}
	}
	if n := countPrefix(names, "sample_report_cloud_detail_2026-08-12_"); n < 2 || n > 2 {
		t.Errorf("2026-08-12 夜页数 = %d, want 2（12 行 10 列，紧凑卡片后一页应装 6 张）", n)
	}
	if n := countPrefix(names, "sample_report_cloud_detail_2026-08-13_"); n < 2 || n > 2 {
		t.Errorf("2026-08-13 夜页数 = %d, want 2（8 行 10 列，紧凑卡片后 6+2）", n)
	}
	if n := countPrefix(names, "sample_report_transparency"); n < 1 {
		t.Errorf("核心窗口至少应产出 1 张，实际 %d", n)
	}

	if !referenceFontLoaded() {
		t.Logf("当前字体 %q 非对等性基准（Hiragino Sans GB），跳过严格档位断言", FontPath())
		return
	}

	wantExact := []string{
		"sample_report_astro.png",
		"sample_report_cloud_detail_2026-08-12_1.png",
		"sample_report_cloud_detail_2026-08-12_2.png",
		"sample_report_cloud_detail_2026-08-13_1.png",
		"sample_report_cloud_detail_2026-08-13_2.png",
		"sample_report_sites.png",
		"sample_report_transparency_1.png",
		"sample_report_transparency_2.png",
	}
	if len(names) != len(wantExact) {
		t.Errorf("文件数 = %d, want %d（紧凑卡片化后 Go 渲染器对等输出）\n实际：%v",
			len(names), len(wantExact), names)
	}
	for i := range wantExact {
		if i < len(names) && names[i] != wantExact[i] {
			t.Errorf("文件名[%d] = %q, want %q", i, names[i], wantExact[i])
		}
	}

	wantScale := map[string]float64{
		"sample_report_sites.png":                     1.0,
		"sample_report_astro.png":                     1.0,
		"sample_report_transparency_1.png":            1.0,
		"sample_report_transparency_2.png":            1.0,
		"sample_report_cloud_detail_2026-08-12_1.png": 1.0,
		"sample_report_cloud_detail_2026-08-12_2.png": 1.0,
		"sample_report_cloud_detail_2026-08-13_1.png": 1.0,
		"sample_report_cloud_detail_2026-08-13_2.png": 1.0,
	}
	for _, r := range results {
		base := filepath.Base(r.Path)
		want, ok := wantScale[base]
		if !ok {
			continue
		}
		if diff := r.Scale - want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s scale = %.6f, want %.1f（与 Python 版选档不一致）",
				base, r.Scale, want)
		}
	}
}

func TestRenderDouyinPageTitles(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过端到端出图")
	}
	requireFont(t)

	results, err := RenderDouyinDetailed(sampleReport, Options{
		OutDir:   t.TempDir(),
		Sections: "低云海拔评估明细",
		Logger:   discardLogger(),
	})
	if err != nil {
		t.Fatalf("RenderDouyinDetailed: %v", err)
	}

	var page1213 []Result
	for _, r := range results {
		if strings.Contains(r.Path, "2026-08-12") {
			page1213 = append(page1213, r)
		}
	}
	if len(page1213) < 2 {
		t.Fatalf("2026-08-12 夜页数 = %d, want >= 2（自适应装箱应分页）", len(page1213))
	}
	for i, r := range page1213 {
		wantSuffix := fmt.Sprintf("（%d/%d）", i+1, len(page1213))
		if !strings.HasSuffix(r.Title, wantSuffix) {
			t.Errorf("page[%d].Title = %q, want 后缀 %q", i, r.Title, wantSuffix)
		}
		if !strings.HasPrefix(r.Title, "低云海拔评估明细"+NightTitleSep+"2026-08-12 夜") {
			t.Errorf("page[%d].Title 前缀错误：%q", i, r.Title)
		}
	}
}

func TestRenderDouyinStemOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过端到端出图")
	}
	requireFont(t)

	outDir := t.TempDir()
	results, err := RenderDouyinDetailed(sampleReport, Options{
		OutDir:   outDir,
		Sections: "点位列表",
		Stem:     "custom_stem",
		Logger:   discardLogger(),
	})
	if err != nil {
		t.Fatalf("RenderDouyinDetailed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("产出 %d 张, want 1", len(results))
	}
	if got := filepath.Base(results[0].Path); got != "custom_stem_sites.png" {
		t.Errorf("文件名 = %q, want custom_stem_sites.png", got)
	}
}

func TestRenderDouyinCreatesOutDir(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过端到端出图")
	}
	requireFont(t)

	outDir := filepath.Join(t.TempDir(), "a", "b", "c")
	if _, err := RenderDouyin(sampleReport, Options{
		OutDir:   outDir,
		Sections: "点位列表",
		Logger:   discardLogger(),
	}); err != nil {
		t.Fatalf("RenderDouyin: %v", err)
	}
	if fi, err := os.Stat(outDir); err != nil || !fi.IsDir() {
		t.Errorf("输出目录未被创建：%v", err)
	}
}

func TestRenderDouyinMatchesDetailed(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过端到端出图")
	}
	requireFont(t)

	opts := Options{Sections: "点位列表,天文条件", Logger: discardLogger()}

	opts.OutDir = t.TempDir()
	plain, err := RenderDouyin(sampleReport, opts)
	if err != nil {
		t.Fatalf("RenderDouyin: %v", err)
	}

	opts.OutDir = t.TempDir()
	detailed, err := RenderDouyinDetailed(sampleReport, opts)
	if err != nil {
		t.Fatalf("RenderDouyinDetailed: %v", err)
	}

	if len(plain) != len(detailed) {
		t.Fatalf("数量不一致：%d vs %d", len(plain), len(detailed))
	}
	for i := range plain {
		if filepath.Base(plain[i]) != filepath.Base(detailed[i].Path) {
			t.Errorf("[%d] %q vs %q", i, plain[i], detailed[i].Path)
		}
	}
}

func TestRenderToCanvasScalesDown(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过渲染")
	}
	requireFont(t)

	mkBlocks := func(n int) []Block {
		rows := make([][]string, n)
		for i := range rows {
			rows[i] = []string{"点位名称", "1650", "6/6", "云海在脚下，整夜可拍"}
		}
		return []Block{{
			Kind:   BlockTable,
			Header: []string{"点位", "海拔m", "有效h", "结论"},
			Rows:   rows,
		}}
	}

	_, smallScale := RenderToCanvas("测试", mkBlocks(3))
	_, bigScale := RenderToCanvas("测试", mkBlocks(40))

	if smallScale != 1.0 {
		t.Errorf("3 行表 scale = %.4f, want 1.0（应无需缩放）", smallScale)
	}
	if bigScale >= smallScale {
		t.Errorf("40 行表 scale = %.4f 未小于 3 行表 %.4f", bigScale, smallScale)
	}

	if bigScale < HardFloorScale*0.9 {
		t.Errorf("scale = %.4f 低于兜底下限 %.4f", bigScale, HardFloorScale*0.9)
	}
}

func TestRenderToCanvasDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过渲染")
	}
	requireFont(t)

	blocks := []Block{{
		Kind:   BlockTable,
		Header: []string{"点位", "结论"},
		Rows:   [][]string{{"牵牛岗", "✅通透"}, {"百丈岭", "⚠️风险"}},
	}}
	_, a := RenderToCanvas("测试", blocks)
	_, b := RenderToCanvas("测试", blocks)
	if a != b {
		t.Errorf("缩放档位不可复现：%.6f vs %.6f", a, b)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 %s: %v", path, err)
	}
}

func assertValidPNG(t *testing.T, path string) {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Errorf("打开 %s: %v", path, err)
		return
	}
	defer fh.Close()

	cfg, err := png.DecodeConfig(fh)
	if err != nil {
		t.Errorf("%s 不是合法 PNG: %v", filepath.Base(path), err)
		return
	}
	if cfg.Width != CanvasW || cfg.Height != CanvasH {
		t.Errorf("%s PNG 头尺寸 = %dx%d, want %dx%d",
			filepath.Base(path), cfg.Width, cfg.Height, CanvasW, CanvasH)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func countPrefix(list []string, prefix string) int {
	n := 0
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}
