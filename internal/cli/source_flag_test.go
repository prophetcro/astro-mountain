package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
)

func TestSourceFlag_DefaultsToOpenMeteo(t *testing.T) {
	o := mustParse(t, "--peak", "2026-08-12")
	if err := o.Validate(); err != nil {
		t.Fatalf("不传 --source 不该校验失败：%v", err)
	}
	if o.Source != "" {
		t.Errorf("未传参时 Options.Source 原始值应为空串，得到 %q", o.Source)
	}
	if got := o.ResolveSource(); got != core.SourceOpenMeteo {
		t.Errorf("默认数据源应为 %q，得到 %q", core.SourceOpenMeteo, got)
	}

	params := o.BuildRunParams(config.Default(), nil)
	if params.Source != core.SourceOpenMeteo {
		t.Errorf("RunParams.Source 应为 %q，得到 %q",
			core.SourceOpenMeteo, params.Source)
	}
}

func TestSourceFlag_AcceptsBothSources(t *testing.T) {
	cases := []struct {
		arg  string
		want core.Source
	}{
		{"openmeteo", core.SourceOpenMeteo},
		{"tomorrow", core.SourceTomorrow},
		{"Tomorrow", core.SourceTomorrow},
		{"  tomorrow", core.SourceTomorrow},
		{"OPENMETEO", core.SourceOpenMeteo},
	}
	for _, c := range cases {
		o := mustParse(t, "--peak", "2026-08-12", "--source", c.arg)
		if err := o.Validate(); err != nil {
			t.Errorf("--source %q 不该校验失败：%v", c.arg, err)
			continue
		}
		if got := o.BuildRunParams(config.Default(), nil).Source; got != c.want {
			t.Errorf("--source %q → RunParams.Source = %q，期望 %q",
				c.arg, got, c.want)
		}
	}
}

func TestSourceFlag_InvalidExitsTwo(t *testing.T) {
	bad := []string{"tommorow", "open-meteo", "auto", "none", "om"}

	for _, v := range bad {
		args := []string{"--peak", "2026-08-12", "--source", v}

		o := mustParse(t, args...)
		err := o.Validate()
		if err == nil {
			t.Errorf("--source %q 必须校验失败，却放行了（解析成 %q）",
				v, o.ResolveSource())
			continue
		}
		if !strings.Contains(err.Error(), "--source") {
			t.Errorf("--source %q 的错误应点名参数名，实际为 %v", v, err)
		}

		for _, s := range core.AllSources() {
			if !strings.Contains(err.Error(), string(s)) {
				t.Errorf("--source %q 的错误未列出可选值 %q：%v", v, s, err)
			}
		}

		var out, errOut bytes.Buffer
		if code := MainWith(args, "dev", &out, &errOut, false); code != ExitUsage {
			t.Errorf("--source %q：退出码应为 %d，实际 %d",
				v, ExitUsage, code)
		}
		if !strings.Contains(errOut.String(), "参数错误") {
			t.Errorf("--source %q：stderr 应有中文错误提示，实际：%s",
				v, errOut.String())
		}

		if strings.TrimSpace(out.String()) != "" {
			t.Errorf("--source %q：参数错误时不该往 stdout 写东西，实际：%s",
				v, out.String())
		}
	}
}

func TestSourceFlag_EmptyValueFallsBackQuietly(t *testing.T) {
	o := mustParse(t, "--peak", "2026-08-12", "--source", "")
	if err := o.Validate(); err != nil {
		t.Fatalf("--source \"\" 应等价于没传，不该报错：%v", err)
	}
	if got := o.ResolveSource(); got != core.DefaultSource {
		t.Errorf("空值应回落 %q，得到 %q", core.DefaultSource, got)
	}
}

func TestSourceFlag_DocumentedInHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := MainWith([]string{"--help"}, "dev", &out, &errOut, false); code != 0 {
		t.Fatalf("--help 退出码应为 0，实际 %d", code)
	}
	help := out.String()

	for _, want := range []string{
		"--source",
		"openmeteo",
		"tomorrow",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("--help 未提到 %q", want)
		}
	}

	if !strings.Contains(help, "平原") {
		t.Errorf("--help 应说明 Tomorrow.io 适合开阔平原，实际：\n%s", help)
	}

	if !strings.Contains(help, "默认") {
		t.Errorf("--help 应写明默认数据源")
	}
}

func TestSourceFlag_IsBusinessFlag(t *testing.T) {
	if !businessFlags["source"] {
		t.Fatal("businessFlags 缺少 \"source\"，只传 --source 时会被误判为无参数而进菜单")
	}

	o := mustParse(t, "--source", "tomorrow")
	if err := o.Validate(); err != nil {
		t.Fatalf("只传 --source 不该校验失败：%v", err)
	}

	if o.ShouldEnterMenu(true) {
		t.Error("显式传了 --source 就是明确的业务意图，不该再进交互菜单")
	}
}
