package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
)

func TestRendererName(t *testing.T) {
	if got := New(config.DouyinConfig{}).Name(); got != "douyin" {
		t.Fatalf("Name() = %q，期望 \"douyin\"", got)
	}
}

func TestOptionsMapping_SectionsJoined(t *testing.T) {
	r := New(config.DouyinConfig{})
	opts := r.options(config.DouyinConfig{
		Sections: []string{"点位列表", "天文条件"},
		FontPath: "/tmp/a.ttf",
	}, "/tmp/out")

	if opts.Sections != "点位列表,天文条件" {
		t.Errorf("Sections = %q，期望 \"点位列表,天文条件\"", opts.Sections)
	}
	if opts.FontPath != "/tmp/a.ttf" {
		t.Errorf("FontPath = %q", opts.FontPath)
	}
	if opts.OutDir != "/tmp/out" {
		t.Errorf("OutDir = %q", opts.OutDir)
	}
}

func TestOptionsMapping_EmptySectionsFallBack(t *testing.T) {
	r := New(config.DouyinConfig{})
	opts := r.options(config.DouyinConfig{}, "/tmp/out")
	if opts.Sections != "" {
		t.Fatalf("空配置应产生空 Sections（由 sectionKeywords 回落），实际 %q", opts.Sections)
	}
	got := opts.sectionKeywords()
	want := strings.Split(DefaultSections, ",")
	if len(got) != len(want) {
		t.Fatalf("回落后的小节数 = %d，期望 %d（%v）", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("小节[%d] = %q，期望 %q", i, got[i], want[i])
		}
	}
}

func TestOptionsMapping_BlankSectionsFiltered(t *testing.T) {
	r := New(config.DouyinConfig{})
	opts := r.options(config.DouyinConfig{
		Sections: []string{"  ", "天文条件", ""},
	}, "")
	if opts.Sections != "天文条件" {
		t.Fatalf("Sections = %q，期望过滤掉空白项后只剩 \"天文条件\"", opts.Sections)
	}
}

func TestOptionsMapping_ConstructorFallback(t *testing.T) {
	r := New(config.DouyinConfig{
		Sections: []string{"核心窗口"},
		FontPath: "/ctor/font.ttf",
	})
	opts := r.options(config.DouyinConfig{}, "/tmp/out")
	if opts.Sections != "核心窗口" {
		t.Errorf("Sections 应回落到构造配置，实际 %q", opts.Sections)
	}
	if opts.FontPath != "/ctor/font.ttf" {
		t.Errorf("FontPath 应回落到构造配置，实际 %q", opts.FontPath)
	}

	opts2 := r.options(config.DouyinConfig{
		Sections: []string{"点位列表"},
		FontPath: "/call/font.ttf",
	}, "/tmp/out")
	if opts2.Sections != "点位列表" || opts2.FontPath != "/call/font.ttf" {
		t.Errorf("传入配置应覆盖构造配置，实际 Sections=%q FontPath=%q",
			opts2.Sections, opts2.FontPath)
	}
}

func TestRender_MissingReportReturnsError(t *testing.T) {
	r := New(config.DouyinConfig{})
	missing := filepath.Join(t.TempDir(), "not_exist.md")
	paths, err := r.Render(missing, config.Default(), t.TempDir())
	if err == nil {
		t.Fatal("报告不存在时应返回 error")
	}
	if len(paths) != 0 {
		t.Fatalf("失败时不应返回任何路径，实际 %v", paths)
	}
}

func TestRender_EndToEnd(t *testing.T) {
	if _, err := ResolveFontPath(""); err != nil {
		t.Skipf("环境无可用中文字体，跳过端到端出图：%v", err)
	}

	md := "# 观测报告\n\n## 点位列表\n\n" +
		"| 点位 | 海拔 |\n| --- | --- |\n| 天荒坪 | 1000 |\n| 括苍山 | 1382 |\n"
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "astro_report_test.md")
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		t.Fatalf("写测试报告失败：%v", err)
	}

	cfg := config.Default()
	cfg.Douyin.Sections = []string{"点位列表"}
	outDir := filepath.Join(dir, "douyin")

	paths, err := New(cfg.Douyin).Render(mdPath, cfg, outDir)
	if err != nil {
		t.Fatalf("Render 失败：%v", err)
	}
	if len(paths) == 0 {
		t.Fatal("Render 成功却没有返回任何图片路径")
	}
	for _, p := range paths {
		info, statErr := os.Stat(p)
		if statErr != nil {
			t.Fatalf("返回的路径不存在：%s：%v", p, statErr)
		}
		if info.Size() == 0 {
			t.Fatalf("生成了空文件：%s", p)
		}
		if filepath.Dir(p) != outDir {
			t.Fatalf("图片未落在指定 outDir：%s，期望目录 %s", p, outDir)
		}
	}
}
