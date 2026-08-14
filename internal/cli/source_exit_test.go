package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
)

type stubTomorrowFetcher struct{}

func (stubTomorrowFetcher) Name() string { return "stub" }

func (stubTomorrowFetcher) FetchSite(context.Context, core.Site) (
	[]dualtrack.HourInput, string, bool, error) {
	panic("闸门漏了：--source tomorrow 不可用时不该走到取数")
}

func newStubTomorrowFactory() func(config.Config) core.TomorrowFetcher {
	return func(config.Config) core.TomorrowFetcher { return stubTomorrowFetcher{} }
}

type stubMeteoblueFetcher struct{}

func (stubMeteoblueFetcher) Name() string { return "stub-meteoblue" }

func (stubMeteoblueFetcher) FetchSite(context.Context, core.Site, time.Time, time.Time,
	map[string]bool) ([]model.HourRow, error) {
	panic("闸门漏了：--source meteoblue 不可用时不该走到取数")
}

func newStubMeteoblueFactory() func(config.Config) core.MeteoblueFetcher {
	return func(config.Config) core.MeteoblueFetcher { return stubMeteoblueFetcher{} }
}

func writeTempConfigMeteoblue(t *testing.T, enabled bool, apiKey string) string {
	t.Helper()

	en := "false"
	if enabled {
		en = "true"
	}
	body := `{"api":{"meteoblue_enabled":` + en +
		`,"meteoblue_api_key":"` + apiKey + `"}}`

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写临时配置失败：%v", err)
	}
	return path
}

func writeTempConfig(t *testing.T, tomorrowEnabled bool, apiKey string) string {
	t.Helper()

	enabled := "false"
	if tomorrowEnabled {
		enabled = "true"
	}
	body := `{"api":{"tomorrow_enabled":` + enabled +
		`,"tomorrow_api_key":"` + apiKey + `"}}`

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写临时配置失败：%v", err)
	}
	return path
}

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()

	var outBuf, errBuf bytes.Buffer
	code = MainWith(args, "qa-test", &outBuf, &errBuf, false)
	return code, outBuf.String(), errBuf.String()
}

func assertRefusedCleanly(t *testing.T, scenario string, code int, stdout string) {
	t.Helper()

	if code != ExitUsage {
		t.Errorf(`[%s] 退出码 = %d，期望 %d。
%d = 参数/配置错误（重试无用，让脚本立刻停）；
1 会被理解成"运行期失败，可以重试"；0 会让 CI 认为这轮成功。`,
			scenario, code, ExitUsage, ExitUsage)
	}

	if len(stdout) != 0 {
		t.Errorf(`[%s] stdout 有 %d 字节输出，期望**严格 0 字节**：
%q

用户很可能这样用：astro-mountain --source tomorrow > report.md
stdout 只要非空，report.md 就会被创建，内容要么是残缺的 A 轨结果、要么是
噪声——两种都比空文件糟，空文件至少一眼看得出没跑成。`,
			scenario, len(stdout), stdout)
	}
}

func TestSourceTomorrowRefusalIsAttributedToTheRightLayer(t *testing.T) {

	t.Setenv("TOMORROW_API_KEY", "")

	cases := []struct {
		scenario string
		enabled  bool
		key      string

		registerFetcher bool
		want            string
		wantNot         []string
	}{
		{
			scenario: "总开关关闭",
			enabled:  false,

			key:             "dummy-key-never-used",
			registerFetcher: true,
			want:            "api.tomorrow_enabled",
			wantNot:         []string{"未配置 Tomorrow.io API key", "尚未接通"},
		},
		{
			scenario:        "key缺失",
			enabled:         true,
			key:             "",
			registerFetcher: true,
			want:            "未配置 Tomorrow.io API key",
			wantNot:         []string{"api.tomorrow_enabled 为 false", "尚未接通"},
		},
		{

			scenario:        "配置齐备但构建未注入取数器",
			enabled:         true,
			key:             "dummy-key-present",
			registerFetcher: false,
			want:            "不是你的配置错",
			wantNot:         []string{"未配置 Tomorrow.io API key", "api.tomorrow_enabled 为 false"},
		},
	}

	for _, c := range cases {
		t.Run(c.scenario, func(t *testing.T) {
			saved := TomorrowFetcherFactory
			if c.registerFetcher {
				TomorrowFetcherFactory = newStubTomorrowFactory()
			} else {
				TomorrowFetcherFactory = nil
			}
			t.Cleanup(func() { TomorrowFetcherFactory = saved })

			code, out, errOut := runCLI(t,
				"--peak", "2026-08-12",
				"--source", "tomorrow",
				"--no-report", "--no-douyin",
				"--config", writeTempConfig(t, c.enabled, c.key),
			)

			assertRefusedCleanly(t, c.scenario, code, out)

			if !strings.Contains(errOut, c.want) {
				t.Errorf(`[%s] stderr 没提到本层的专属原因 %q，用户不知道该改什么。
实际 stderr：
%s`, c.scenario, c.want, errOut)
			}
			for _, no := range c.wantNot {
				if strings.Contains(errOut, no) {
					t.Errorf(`[%s] stderr 混进了**其它层**的原因 %q。
原因归错层会把用户引去做无用功（例如为"链路没接通"跑去申请密钥）。
实际 stderr：
%s`, c.scenario, no, errOut)
				}
			}

			if !strings.Contains(errOut, "--source openmeteo") {
				t.Errorf("[%s] stderr 没给出退路（期望提到 --source openmeteo）：\n%s",
					c.scenario, errOut)
			}
		})
	}
}

func TestSourceTomorrowRefusalNeverLeaksATrackOutput(t *testing.T) {
	t.Setenv("TOMORROW_API_KEY", "")

	saved := TomorrowFetcherFactory
	TomorrowFetcherFactory = newStubTomorrowFactory()
	t.Cleanup(func() { TomorrowFetcherFactory = saved })

	code, out, errOut := runCLI(t,
		"--peak", "2026-08-12",
		"--source", "tomorrow",
		"--no-report", "--no-douyin",
		"--config", writeTempConfig(t, true, ""),
	)
	if code != ExitUsage {
		t.Fatalf("前置条件不满足：退出码 = %d，期望 %d", code, ExitUsage)
	}

	leaks := []string{
		aTrackMarker,
		"观测夜",
		"云底相对机位",
	}
	combined := out + errOut
	for _, sig := range leaks {
		if strings.Contains(combined, sig) {
			t.Errorf(`B 轨被拒后仍泄漏了 A 轨产物特征 %q。

用户显式要的是 Tomorrow.io。此时输出任何 Open-Meteo 结果，他都可能当成
B 轨结论读走——D4-6 第 4 条把"用户以为在看 B 轨、实际是 A 轨"
定性为本系统最坏的失败模式。

--- stdout ---
%s
--- stderr ---
%s`, sig, out, errOut)
		}
	}
}

func TestTomorrowDeliverableImpliesWired(t *testing.T) {

	cfg := config.Default()

	for _, tc := range []struct {
		name      string
		attach    bool
		wantWired bool
	}{
		{"未挂取数器", false, false},
		{"已挂取数器", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := core.NewEngine(cfg)
			if tc.attach {
				engine.TomorrowFetcher = stubTomorrowFetcher{}
			}

			if got := engine.TomorrowWired(); got != tc.wantWired {
				t.Fatalf("TomorrowWired() = %v，期望 %v", got, tc.wantWired)
			}
			if engine.TomorrowDeliverable() && !engine.TomorrowWired() {
				t.Errorf(`TomorrowDeliverable() = true 但 TomorrowWired() = false。
"能交付 B 轨报告"却"没有取数器"是自相矛盾的：闸门会放行一次注定取不到数的
运行，接着不是空指针就是静默回落到 A 轨。`)
			}
		})
	}

	var nilEngine *core.Engine
	if nilEngine.TomorrowWired() {
		t.Error("nil Engine 的 TomorrowWired() 返回了 true")
	}
}

func TestSourceMeteoblueRefusalIsAttributedToTheRightLayer(t *testing.T) {
	t.Setenv("METEOBLUE_API_KEY", "")

	cases := []struct {
		scenario string
		enabled  bool
		key      string

		registerFetcher bool
		want            string
		wantNot         []string
	}{
		{
			scenario:        "总开关关闭",
			enabled:         false,
			key:             "dummy-key-never-used",
			registerFetcher: true,
			want:            "api.meteoblue_enabled",
			wantNot:         []string{"未配置 Meteoblue API key", "尚未接通"},
		},
		{
			scenario:        "key缺失",
			enabled:         true,
			key:             "",
			registerFetcher: true,
			want:            "未配置 Meteoblue API key",
			wantNot:         []string{"api.meteoblue_enabled 为 false", "尚未接通"},
		},
		{
			scenario:        "配置齐备但构建未注入取数器",
			enabled:         true,
			key:             "dummy-key-present",
			registerFetcher: false,
			want:            "不是你的配置错",
			wantNot:         []string{"未配置 Meteoblue API key", "api.meteoblue_enabled 为 false"},
		},
	}

	for _, c := range cases {
		t.Run(c.scenario, func(t *testing.T) {
			saved := MeteoblueFetcherFactory
			if c.registerFetcher {
				MeteoblueFetcherFactory = newStubMeteoblueFactory()
			} else {
				MeteoblueFetcherFactory = nil
			}
			t.Cleanup(func() { MeteoblueFetcherFactory = saved })

			code, out, errOut := runCLI(t,
				"--peak", "2026-08-12",
				"--source", "meteoblue",
				"--no-report", "--no-douyin",
				"--config", writeTempConfigMeteoblue(t, c.enabled, c.key),
			)

			assertRefusedCleanly(t, c.scenario, code, out)

			if !strings.Contains(errOut, c.want) {
				t.Errorf(`[%s] stderr 没提到本层的专属原因 %q，用户不知道该改什么。
实际 stderr：
%s`, c.scenario, c.want, errOut)
			}
			for _, no := range c.wantNot {
				if strings.Contains(errOut, no) {
					t.Errorf(`[%s] stderr 混进了**其它层**的原因 %q。
原因归错层会把用户引去做无用功。
实际 stderr：
%s`, c.scenario, no, errOut)
				}
			}

			if !strings.Contains(errOut, "--source openmeteo") {
				t.Errorf("[%s] stderr 没给出退路（期望提到 --source openmeteo）：\n%s",
					c.scenario, errOut)
			}
		})
	}
}
