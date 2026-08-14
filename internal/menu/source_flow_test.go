package menu

import (
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
)

func newTestState(t *testing.T) *state {
	t.Helper()
	cfg := config.Default()
	cfg.Output.OutDir = t.TempDir()
	return &state{cfg: cfg}
}

func TestEquivalentCommandSuppressesDefaultSource(t *testing.T) {
	s := newTestState(t)

	for _, src := range []core.Source{core.SourceOpenMeteo, ""} {
		f := &reportForm{
			usePeak: true,
			peak:    "2026-08-12",
			days:    3,
			source:  src,
			models:  s.cfg.API.Models,

			wantMarkdown: true,
			wantDouyin:   true,
			allSites:     true,
		}
		cmd := s.equivalentCommand(f)
		if strings.Contains(cmd, "--source") {
			t.Errorf("source=%q（默认）时等价命令不该出现 --source，实际：%s", src, cmd)
		}
	}
}

func TestEquivalentCommandEmitsNonDefaultSource(t *testing.T) {
	s := newTestState(t)
	f := &reportForm{
		usePeak:      true,
		peak:         "2026-08-12",
		days:         3,
		source:       core.SourceTomorrow,
		models:       s.cfg.API.Models,
		wantMarkdown: true,
		wantDouyin:   true,
		allSites:     true,
	}

	cmd := s.equivalentCommand(f)
	if !strings.Contains(cmd, "--source tomorrow") {
		t.Fatalf("选了 Tomorrow.io 却没打印 --source tomorrow，实际：%s", cmd)
	}

	if i, j := strings.Index(cmd, "--source"), strings.Index(cmd, "--peak"); i < j {
		t.Errorf("--source 不该排在 --peak 之前，实际：%s", cmd)
	}
}

func TestSourceLineCarriesLabelAndHint(t *testing.T) {
	openMeteo := sourceLine(core.SourceOpenMeteo)
	if !strings.Contains(openMeteo, "Open-Meteo") {
		t.Errorf("缺少标签：%q", openMeteo)
	}
	if !strings.Contains(openMeteo, "默认") {
		t.Errorf("默认源必须标注「默认」，实际：%q", openMeteo)
	}
	if !strings.Contains(openMeteo, "云海") {
		t.Errorf("应说明可判脚下云海，实际：%q", openMeteo)
	}

	tomorrow := sourceLine(core.SourceTomorrow)
	if !strings.Contains(tomorrow, "Tomorrow.io") {
		t.Errorf("缺少标签：%q", tomorrow)
	}
	if strings.Contains(tomorrow, "默认") {
		t.Errorf("非默认源不该标注「默认」，实际：%q", tomorrow)
	}

	if !strings.Contains(tomorrow, "平原") {
		t.Errorf("应备注适合开阔平原，实际：%q", tomorrow)
	}
	if !strings.Contains(tomorrow, "配额") {
		t.Errorf("应提示受配额限制，实际：%q", tomorrow)
	}
}

func TestEffectiveSourceZeroValueFallsBack(t *testing.T) {
	var f reportForm
	if got := f.effectiveSource(); got != core.DefaultSource {
		t.Errorf("零值表单的数据源应为 %q，得到 %q", core.DefaultSource, got)
	}
	f.source = core.SourceTomorrow
	if got := f.effectiveSource(); got != core.SourceTomorrow {
		t.Errorf("显式赋值后应为 %q，得到 %q", core.SourceTomorrow, got)
	}
}

func TestNewReportFormDefaultsToOpenMeteo(t *testing.T) {
	s := newTestState(t)
	f := s.newReportForm()
	if got := f.effectiveSource(); got != core.SourceOpenMeteo {
		t.Errorf("新建表单的数据源应为 %q，得到 %q", core.SourceOpenMeteo, got)
	}
}

func TestAdvancedMenuListsSourceFirst(t *testing.T) {

	_, out := runMenu(t, "1\n1\n\n\n\n\nq\ny\n", nil)

	if strings.Contains(out, "步骤 5/5") {
		t.Errorf("不该出现「步骤 5/5」——数据源应并入高级选项而非新增步骤\n%s", out)
	}
	mustContain(t, out, "步骤 4/4")

	wantOrder := []string{
		"[1] 数据源",
		"[2] 气象模式",
		"[3] 输出目录",
		"[4] HTTP 缓存",
		"[5] 详细日志",
		"[6] 终端报表",
	}
	mustContain(t, out, wantOrder...)

	prev := -1
	for _, w := range wantOrder {
		at := strings.Index(out, w)
		if at < 0 {
			t.Fatalf("缺少菜单项 %q", w)
		}
		if at < prev {
			t.Errorf("菜单项 %q 出现位置早于前一项，编号顺延错了\n%s", w, out)
		}
		prev = at
	}
}

func TestAdvancedMenuInvalidIndexMentionsSixOptions(t *testing.T) {

	_, out := runMenu(t, "1\n1\n\n\n\n\n9\n\nq\ny\n", nil)

	if !strings.Contains(out, "1-6") {
		t.Errorf("高级选项非法序号错误文案应提示「1-6」，实际却未出现\n%s", out)
	}

	if strings.Contains(out, "1-5") {
		t.Errorf("高级选项错误文案不应是「1-5」——第 6 项已加入，应为 1-6\n%s", out)
	}
}

func TestSourcePickerWarnsQuotaBeforeChoosing(t *testing.T) {
	_, out := runMenu(t, "1\n1\n\n\n\n\n1\ntomorrow\n\nb\nq\ny\n", nil)

	mustContain(t, out, "500", "25", "3")
	mustContain(t, out, "Tomorrow.io")

}

func TestConfirmSummaryShowsSourceRow(t *testing.T) {

	_, out := runMenu(t, "1\n1\n\n\n\n\n\nb\nq\ny\n", nil)

	mustContain(t, out, "数据源", "Open-Meteo")

	if strings.Contains(out, "--source") {
		t.Errorf("默认数据源时等价命令不该出现 --source\n%s", out)
	}
}
