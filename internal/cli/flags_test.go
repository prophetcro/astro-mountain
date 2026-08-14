package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
)

func fixedNow() time.Time {
	return time.Date(2026, 8, 1, 21, 30, 0, 0, time.UTC)
}

func mustParse(t *testing.T, args ...string) *Options {
	t.Helper()
	o, err := Parse(args)
	if err != nil {
		t.Fatalf("Parse(%v) 意外失败：%v", args, err)
	}
	o.Now = fixedNow
	return o
}

func TestValidate_MutualExclusion(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantFrag string
	}{
		{
			name:     "peak 与 start 互斥",
			args:     []string{"--peak", "2026-08-12", "--start", "2026-08-10"},
			wantFrag: "--peak 与 --start/--end 不能同时使用",
		},
		{
			name:     "peak 与 end 互斥",
			args:     []string{"--peak", "2026-08-12", "--end", "2026-08-15"},
			wantFrag: "--peak 与 --start/--end 不能同时使用",
		},
		{
			name:     "douyin 与 no-douyin 互斥",
			args:     []string{"--peak", "2026-08-12", "--douyin", "--no-douyin"},
			wantFrag: "--douyin 与 --no-douyin 不能同时使用",
		},
		{
			name:     "menu 与 no-menu 互斥",
			args:     []string{"--menu", "--no-menu"},
			wantFrag: "--menu 与 --no-menu 不能同时使用",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := mustParse(t, tc.args...)
			err := o.Validate()
			if err == nil {
				t.Fatalf("期望校验失败，实际通过")
			}
			if !strings.Contains(err.Error(), tc.wantFrag) {
				t.Fatalf("错误信息未命中关键片段\n  期望包含: %s\n  实际: %s",
					tc.wantFrag, err.Error())
			}
		})
	}
}

func TestValidate_DateFormat(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantErr  bool
		wantFrag string
	}{
		{"peak 合法", []string{"--peak", "2026-08-12"}, false, ""},
		{"peak 斜杠分隔非法", []string{"--peak", "2026/08/12"}, true, "--peak 日期格式错误"},
		{"peak 单位数月份非法", []string{"--peak", "2026-8-12"}, true, "--peak 日期格式错误"},
		{"peak 非日期非法", []string{"--peak", "tomorrow"}, true, "--peak 日期格式错误"},
		{"peak 月份越界非法", []string{"--peak", "2026-13-01"}, true, "--peak 日期格式错误"},
		{
			"start 格式非法",
			[]string{"--start", "20260810", "--end", "2026-08-15"},
			true, "--start 日期格式错误",
		},
		{
			"end 格式非法",
			[]string{"--start", "2026-08-10", "--end", "2026-8-15"},
			true, "--end 日期格式错误",
		},
		{
			"合法区间",
			[]string{"--start", "2026-08-10", "--end", "2026-08-15"},
			false, "",
		},
		{
			"start == end 合法",
			[]string{"--start", "2026-08-10", "--end", "2026-08-10"},
			false, "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := mustParse(t, tc.args...)
			err := o.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望校验失败，实际通过")
				}
				if !strings.Contains(err.Error(), tc.wantFrag) {
					t.Fatalf("错误信息未命中\n  期望包含: %s\n  实际: %s",
						tc.wantFrag, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("期望校验通过，实际失败：%v", err)
			}
		})
	}
}

func TestValidate_EndBeforeStart(t *testing.T) {
	o := mustParse(t, "--start", "2026-08-15", "--end", "2026-08-10")
	err := o.Validate()
	if err == nil {
		t.Fatal("期望 --end 早于 --start 被拒绝，实际通过")
	}
	if !strings.Contains(err.Error(), "不能早于") {
		t.Fatalf("错误信息不符：%s", err.Error())
	}
}

func TestValidate_StartEndMustPair(t *testing.T) {
	for _, args := range [][]string{
		{"--start", "2026-08-10"},
		{"--end", "2026-08-15"},
	} {
		o := mustParse(t, args...)
		err := o.Validate()
		if err == nil {
			t.Fatalf("%v：期望「只给一端」被拒绝，实际通过", args)
		}
		if !strings.Contains(err.Error(), "必须成对出现") {
			t.Fatalf("%v：错误信息不符：%s", args, err.Error())
		}
	}
}

func TestValidate_Days(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"days 0 被拒", []string{"--peak", "2026-08-12", "--days", "0"}, true},
		{"days 负数被拒", []string{"--peak", "2026-08-12", "--days", "-3"}, true},
		{"days 1 通过", []string{"--peak", "2026-08-12", "--days", "1"}, false},
		{"days 5 通过", []string{"--peak", "2026-08-12", "--days", "5"}, false},
		{"未给 days 通过", []string{"--peak", "2026-08-12"}, false},
		{"days 无 peak 被拒", []string{"--days", "5"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := mustParse(t, tc.args...)
			err := o.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("期望校验失败，实际通过")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("期望校验通过，实际失败：%v", err)
			}
			if tc.wantErr && err != nil && !strings.Contains(err.Error(), "--days") {
				t.Fatalf("错误信息应提到 --days，实际：%s", err.Error())
			}
		})
	}
}

func TestValidate_RejectsPositionalArgs(t *testing.T) {
	o := mustParse(t, "--peak", "2026-08-12", "extra-arg")
	err := o.Validate()
	if err == nil {
		t.Fatal("期望位置参数被拒绝，实际通过")
	}
	if !strings.Contains(err.Error(), "位置参数") {
		t.Fatalf("错误信息不符：%s", err.Error())
	}
}

func TestParse_UnknownFlag(t *testing.T) {
	if _, err := Parse([]string{"--nonexistent"}); err == nil {
		t.Fatal("期望未知 flag 报错，实际通过")
	}
}

func cfgWithAutoDouyin(auto bool) config.Config {
	cfg := config.Default()
	cfg.Output.AutoDouyin = auto
	return cfg
}

func TestResolveDouyin_Matrix(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		autoDouyin bool
		want       bool
	}{
		{"无 flag + auto=true → 出图", nil, true, true},
		{"无 flag + auto=false → 不出图", nil, false, false},
		{"--douyin + auto=false → 出图", []string{"--douyin"}, false, true},
		{"--douyin + auto=true → 出图", []string{"--douyin"}, true, true},
		{"--no-douyin + auto=true → 不出图", []string{"--no-douyin"}, true, false},
		{"--no-douyin + auto=false → 不出图", []string{"--no-douyin"}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := mustParse(t, tc.args...)
			got := o.ResolveDouyin(cfgWithAutoDouyin(tc.autoDouyin))
			if got != tc.want {
				t.Fatalf("ResolveDouyin = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestDefaultConfig_AutoDouyinEnabled(t *testing.T) {
	if !config.Default().Output.AutoDouyin {
		t.Fatal("内置默认 config.json 的 output.auto_douyin 必须为 true")
	}
	o := mustParse(t, "--peak", "2026-08-12")
	if !o.ResolveDouyin(config.Default()) {
		t.Fatal("不给出图相关 flag 时应按内置默认自动出图")
	}
}

func TestShouldEnterMenu(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		isTTY bool
		want  bool
	}{
		{"无参数 + TTY → 进菜单", nil, true, true},
		{"无参数 + 非 TTY → 不进菜单", nil, false, false},
		{"有业务参数 + TTY → 不进菜单", []string{"--peak", "2026-08-12"}, true, false},
		{"有业务参数 + 非 TTY → 不进菜单", []string{"--csv"}, false, false},
		{"--menu + 非 TTY → 强制进菜单", []string{"--menu"}, false, true},
		{"--menu + 有业务参数 → 强制进菜单", []string{"--menu", "--peak", "2026-08-12"}, true, true},
		{"--no-menu + TTY → 不进菜单", []string{"--no-menu"}, true, false},
		{"--verbose 不算业务参数 + TTY → 进菜单", []string{"--verbose"}, true, true},
		{"--config 不算业务参数 + TTY → 进菜单", []string{"--config", "x.json"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := mustParse(t, tc.args...)
			if got := o.ShouldEnterMenu(tc.isTTY); got != tc.want {
				t.Fatalf("ShouldEnterMenu(%v) = %v，期望 %v", tc.isTTY, got, tc.want)
			}
		})
	}
}

func TestIsImplicitBatch(t *testing.T) {
	if o := mustParse(t); !o.IsImplicitBatch(false) {
		t.Fatal("无参数 + 非 TTY 应被判为隐式批处理，需要 stderr 提示")
	}
	if o := mustParse(t); o.IsImplicitBatch(true) {
		t.Fatal("无参数 + TTY 会进菜单，不该判为隐式批处理")
	}
	if o := mustParse(t, "--peak", "2026-08-12"); o.IsImplicitBatch(false) {
		t.Fatal("显式给了业务参数就不是隐式批处理")
	}

	if o := mustParse(t, "--no-menu"); o.IsImplicitBatch(true) {
		t.Fatal("--no-menu 在 TTY 下不该被判为隐式批处理（提示文案会是错的）")
	}
	if o := mustParse(t, "--no-menu"); o.IsImplicitBatch(false) {
		t.Fatal("--no-menu 是显式表态，不需要额外提示")
	}
	if o := mustParse(t, "--menu"); o.IsImplicitBatch(false) {
		t.Fatal("--menu 是显式表态，不需要额外提示")
	}
}

func TestBuildRunParams_FullMapping(t *testing.T) {
	o := mustParse(t,
		"--peak", "2026-08-12",
		"--days", "3",
		"--models", "gfs_seamless",
		"--sites", "./my_sites.json",
		"--out-dir", "./artifacts",
		"--csv", "--json",
		"--no-report", "--no-cache",
		"--quiet", "--verbose",
		"--douyin",
	)
	if err := o.Validate(); err != nil {
		t.Fatalf("参数应合法，实际：%v", err)
	}

	var buf bytes.Buffer
	p := o.BuildRunParams(cfgWithAutoDouyin(false), &buf)

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"Peak", p.Peak, "2026-08-12"},
		{"Days", p.Days, 3},
		{"Start", p.Start, ""},
		{"End", p.End, ""},
		{"Models", p.Models, "gfs_seamless"},
		{"SitesPath", p.SitesPath, "./my_sites.json"},
		{"OutDir", p.OutDir, "./artifacts"},
		{"ExportCSV", p.ExportCSV, true},
		{"ExportJSON", p.ExportJSON, true},
		{"NoReport", p.NoReport, true},
		{"NoCache", p.NoCache, true},
		{"Quiet", p.Quiet, true},
		{"Verbose", p.Verbose, true},
		{"Douyin", p.Douyin, true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("RunParams.%s = %v，期望 %v", c.field, c.got, c.want)
		}
	}
	if p.Stdout != &buf {
		t.Error("RunParams.Stdout 应为传入的 writer")
	}
}

func TestBuildRunParams_DaysFallsBackToConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Output.DefaultDays = 7

	o := mustParse(t, "--peak", "2026-08-12")
	if got := o.BuildRunParams(cfg, nil).Days; got != 7 {
		t.Fatalf("未指定 --days 时应取配置 default_days=7，实际 %d", got)
	}

	o2 := mustParse(t, "--peak", "2026-08-12", "--days", "2")
	if got := o2.BuildRunParams(cfg, nil).Days; got != 2 {
		t.Fatalf("显式 --days 2 应覆盖配置，实际 %d", got)
	}
}

func TestBuildRunParams_ExplicitRangePreserved(t *testing.T) {
	o := mustParse(t, "--start", "2026-08-10", "--end", "2026-08-15")
	p := o.BuildRunParams(config.Default(), nil)
	if p.Start != "2026-08-10" || p.End != "2026-08-15" {
		t.Fatalf("显式区间被改写：Start=%s End=%s", p.Start, p.End)
	}
	if p.Peak != "" {
		t.Fatalf("未给 --peak 时 Peak 应为空，实际 %q", p.Peak)
	}
}

func TestBuildRunParams_NoDateFallsBackToToday(t *testing.T) {
	cfg := config.Default()
	cfg.Output.DefaultDays = 5

	o := mustParse(t)
	p := o.BuildRunParams(cfg, nil)

	if p.Peak != "" {
		t.Fatalf("默认区间不应设置 Peak，实际 %q", p.Peak)
	}
	if p.Start != "2026-08-01" {
		t.Fatalf("默认起始日应为冻结的今天 2026-08-01，实际 %q", p.Start)
	}
	if p.End != "2026-08-06" {
		t.Fatalf("默认结束日应为今天 +5 天 2026-08-06，实际 %q", p.End)
	}
}

func TestMainWith_Help(t *testing.T) {
	var out, errOut bytes.Buffer
	code := MainWith([]string{"--help"}, "v1.2.3", &out, &errOut, true)
	if code != ExitOK {
		t.Fatalf("--help 退出码应为 0，实际 %d", code)
	}
	text := out.String()
	for _, frag := range []string{
		"astro-mountain v1.2.3", "--peak", "--days", "--start", "--end",
		"--models", "--sites", "--config", "--out-dir", "--csv", "--json",
		"--no-report", "--no-cache", "--douyin", "--no-douyin",
		"--menu", "--no-menu", "--quiet", "--verbose", "--version", "--help",
		"示例：", "退出码：",
	} {
		if !strings.Contains(text, frag) {
			t.Errorf("帮助文本缺少 %q", frag)
		}
	}
	if errOut.Len() != 0 {
		t.Errorf("--help 不该往 stderr 写东西，实际：%s", errOut.String())
	}
}

func TestMainWith_HelpShorthand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := MainWith([]string{"-h"}, "dev", &out, &errOut, true); code != ExitOK {
		t.Fatalf("-h 退出码应为 0，实际 %d", code)
	}
	if !strings.Contains(out.String(), "用法：") {
		t.Fatal("-h 应输出与 --help 相同的帮助文本")
	}
}

func TestMainWith_Version(t *testing.T) {
	var out, errOut bytes.Buffer
	code := MainWith([]string{"--version"}, "v9.9.9", &out, &errOut, true)
	if code != ExitOK {
		t.Fatalf("--version 退出码应为 0，实际 %d", code)
	}
	if got := strings.TrimSpace(out.String()); got != "astro-mountain v9.9.9" {
		t.Fatalf("版本输出不符：%q", got)
	}
}

func TestMainWith_ValidationErrorExitsTwo(t *testing.T) {
	cases := [][]string{
		{"--peak", "2026-08-12", "--start", "2026-08-10"},
		{"--peak", "2026/08/12"},
		{"--peak", "2026-08-12", "--days", "0"},
		{"--douyin", "--no-douyin"},
		{"--menu", "--no-menu"},
		{"--start", "2026-08-15", "--end", "2026-08-10"},
		{"--totally-unknown-flag"},
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		code := MainWith(args, "dev", &out, &errOut, false)
		if code != ExitUsage {
			t.Errorf("%v：退出码应为 2，实际 %d", args, code)
		}
		if !strings.Contains(errOut.String(), "参数错误") {
			t.Errorf("%v：stderr 应包含中文错误提示，实际：%s", args, errOut.String())
		}
	}
}

func TestMenuHook_DefaultsToNil(t *testing.T) {
	if MenuAvailable() != (MenuRunner != nil) {
		t.Fatal("MenuAvailable 应如实反映 MenuRunner 是否已注入")
	}
}
