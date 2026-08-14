package core

import (
	"context"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
)

type fakeTomorrowFetcher struct{}

func (fakeTomorrowFetcher) Name() string { return "fake" }

func (fakeTomorrowFetcher) FetchSite(context.Context, Site) (
	[]dualtrack.HourInput, string, bool, error) {
	return nil, dualtrack.DatumAGL, true, nil
}

func TestTomorrowUnwiredMustSpeakUp(t *testing.T) {
	isolateAPIKeyEnv(t)

	cfg := cfgWithKey(true, "dummy-key-for-test")

	reason := TomorrowUnavailableReason(SourceTomorrow, cfg, false)
	if reason == "" {
		t.Errorf(`静默降级（D4-6 红线 4）：用户显式选了 B 轨、配置与密钥都齐备，
但本次运行没有注入 TomorrowFetcher（wired=false），B 轨不可能取到任何数据；
而 TomorrowUnavailableReason 返回空串，等于系统对外宣称"B 轨可用"。

用户实际会经历：
    astro-mountain --peak 2026-08-12 --source tomorrow
  → 参数校验通过、菜单/确认页/等效命令都显示 Tomorrow.io
  → 退出码 0，产出一份 Open-Meteo 报告（ReportMeta.Source 写死
    "Open-Meteo free API"，见 internal/core/kernel.go:281）
  → 全程零警告

设计文档 D4-6 第 4 条原文：「用户以为在看 B 轨、实际是 A 轨，
是本系统最坏的失败模式」。

修法：恢复 TomorrowUnavailableReason 里的 if !wired 分支。`)
	}

	if reason != "" && !strings.Contains(reason, "openmeteo") {
		t.Errorf("未接线的原因文本没有给出可执行的出路（期望提到 openmeteo）：%q", reason)
	}
}

func TestTomorrowWiredAndConfiguredIsAvailable(t *testing.T) {
	isolateAPIKeyEnv(t)

	cfg := cfgWithKey(true, "dummy-key-for-test")

	if reason := TomorrowUnavailableReason(SourceTomorrow, cfg, true); reason != "" {
		t.Errorf(`配置齐备且已注入取数器，B 轨却仍被判不可用：%q
这会让 B 轨在任何情况下都跑不起来。`, reason)
	}
}

func TestOpenMeteoNeverBlockedByTomorrowWiring(t *testing.T) {
	isolateAPIKeyEnv(t)

	cases := []struct {
		name  string
		cfg   config.Config
		wired bool
	}{
		{"未接线+未配置", cfgWithKey(false, ""), false},
		{"未接线+已配置", cfgWithKey(true, "dummy-key-for-test"), false},
		{"已接线+未配置", cfgWithKey(false, ""), true},
		{"已接线+已配置", cfgWithKey(true, "dummy-key-for-test"), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if r := TomorrowUnavailableReason(SourceOpenMeteo, c.cfg, c.wired); r != "" {
				t.Errorf(`A 轨（默认轨）被 B 轨的接线状态挡住了：%q
A 轨的可用性必须与 B 轨完全无关，否则默认路径与 parity 回归一起失守。`, r)
			}
		})
	}
}

func TestEngineTomorrowWiredNilSafe(t *testing.T) {
	var nilEngine *Engine
	if nilEngine.TomorrowWired() {
		t.Error("nil Engine 不可能接线，TomorrowWired() 却返回 true")
	}

	if (&Engine{}).TomorrowWired() {
		t.Error("TomorrowFetcher 为 nil 时 TomorrowWired() 必须为 false——" +
			"否则 --source tomorrow 会被放行进一条取不到数的路径")
	}

	if !(&Engine{TomorrowFetcher: fakeTomorrowFetcher{}}).TomorrowWired() {
		t.Error("已注入 TomorrowFetcher，TomorrowWired() 却返回 false——" +
			"B 轨将永远无法启用")
	}
}
