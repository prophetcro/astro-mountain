package core

import (
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/api/tomorrow"
	"github.com/prophetcro/astro-mountain/internal/config"
)

func cfgWith(enabled bool) config.Config {
	return cfgWithKey(enabled, "test-key-not-a-real-secret")
}

func cfgWithKey(enabled bool, key string) config.Config {
	c := config.Default()
	c.API.TomorrowEnabled = enabled
	c.API.TomorrowAPIKey = key
	return c
}

func isolateAPIKeyEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envTomorrowAPIKey, "")
}

func TestOpenMeteoNeverTouchesTomorrow(t *testing.T) {

	notTomorrow := []Source{
		SourceOpenMeteo,
		"",
		"openmeteo",
		"OpenMeteo",
		"open-meteo",
	}

	for _, enabled := range []bool{true, false} {
		cfg := cfgWith(enabled)
		for _, src := range notTomorrow {
			if UseTomorrow(src, cfg) {
				t.Errorf("source=%q tomorrow_enabled=%v：UseTomorrow 返回 true，"+
					"红线破了——用户没选 B 轨却会被发起请求/扣配额",
					src, enabled)
			}

			for _, wired := range []bool{true, false} {
				if r := TomorrowUnavailableReason(src, cfg, wired); r != "" {
					t.Errorf("source=%q wired=%v：不该产出 B 轨不可用提示，得到 %q",
						src, wired, r)
				}
			}
		}
	}
}

func TestUseTomorrowShortCircuitsBeforeConfig(t *testing.T) {
	on, off := cfgWith(true), cfgWith(false)

	if UseTomorrow(SourceOpenMeteo, on) || UseTomorrow(SourceOpenMeteo, off) {
		t.Fatal("openmeteo 下 UseTomorrow 必须无条件 false，说明配置判断跑到了 Source 前面")
	}

	if !UseTomorrow(SourceTomorrow, on) {
		t.Error("tomorrow + tomorrow_enabled=true 应放行")
	}
	if UseTomorrow(SourceTomorrow, off) {
		t.Error("tomorrow + tomorrow_enabled=false 必须拦下")
	}
}

func TestTomorrowUnavailableReasonSpeaksUp(t *testing.T) {
	isolateAPIKeyEnv(t)

	r := TomorrowUnavailableReason(SourceTomorrow, cfgWithKey(false, ""), true)
	if r == "" {
		t.Fatal("选了 B 轨但配置关着，必须给出原因，不能静默降级")
	}

	if !strings.Contains(r, "api.tomorrow_enabled") {
		t.Errorf("提示应点名配置项 api.tomorrow_enabled，实际为 %q", r)
	}
	if !strings.Contains(r, "--source openmeteo") {
		t.Errorf("提示应给出退路 --source openmeteo，实际为 %q", r)
	}
}

func TestTomorrowUnavailableReasonCoversMissingKey(t *testing.T) {
	t.Run("开关开着但没密钥_必须出声且说清怎么配", func(t *testing.T) {
		isolateAPIKeyEnv(t)

		r := TomorrowUnavailableReason(SourceTomorrow, cfgWithKey(true, ""), true)
		if r == "" {
			t.Fatal("tomorrow_enabled=true 但 key 为空时返回了空串——" +
				"这等于宣称 B 轨可用，用户会静默拿到 A 轨报告（D4-6 红线 4）")
		}

		for _, want := range []string{
			envTomorrowAPIKey,
			"api.tomorrow_api_key",
			"--source openmeteo",
		} {
			if !strings.Contains(r, want) {
				t.Errorf("密钥缺失提示应包含 %q（用户才知道怎么修），实际为 %q", want, r)
			}
		}
	})

	t.Run("密钥来自环境变量_不该再被判成没配", func(t *testing.T) {
		t.Setenv(envTomorrowAPIKey, "key-from-env")

		r := TomorrowUnavailableReason(SourceTomorrow, cfgWithKey(true, ""), true)
		if strings.Contains(r, "未配置 Tomorrow.io API key") {
			t.Errorf("env 里已有 key，却仍报「未配置密钥」：%q", r)
		}

		if !UseTomorrow(SourceTomorrow, cfgWithKey(true, "")) {
			t.Error("env 里已有 key，闸门却关着——" +
				"用 env 配 key 是文档推荐的主路径，被判死等于这条路不能走")
		}
	})

	t.Run("纯空白的密钥等于没配", func(t *testing.T) {
		t.Setenv(envTomorrowAPIKey, "   ")

		r := TomorrowUnavailableReason(SourceTomorrow, cfgWithKey(true, "\t\n "), true)
		if !strings.Contains(r, envTomorrowAPIKey) {
			t.Errorf("全空白的 key 应视同没配并给出配置指引，实际为 %q", r)
		}
	})
}

func TestGateAndReasonNeverContradict(t *testing.T) {
	isolateAPIKeyEnv(t)

	cfgs := map[string]config.Config{
		"开关关_无密钥": cfgWithKey(false, ""),
		"开关关_有密钥": cfgWithKey(false, "k"),
		"开关开_无密钥": cfgWithKey(true, ""),
		"开关开_有密钥": cfgWithKey(true, "k"),
	}
	for name, cfg := range cfgs {
		for _, src := range []Source{SourceTomorrow, SourceOpenMeteo, ""} {
			for _, wired := range []bool{true, false} {
				gate := UseTomorrow(src, cfg)
				reason := TomorrowUnavailableReason(src, cfg, wired)

				if src != SourceTomorrow {
					if gate {
						t.Errorf("[%s] source=%q：非 B 轨时闸门必须关", name, src)
					}
					if reason != "" {
						t.Errorf("[%s] source=%q wired=%v：非 B 轨时不该有抱怨，得到 %q",
							name, src, wired, reason)
					}
					continue
				}
				if !gate && reason == "" {
					t.Errorf("[%s] source=tomorrow wired=%v：闸门关着却一言不发——"+
						"CLI 会据此认为 B 轨没问题，然后静默跑 A 轨（D4-6 红线 4）",
						name, wired)
				}

				if !wired && reason == "" {
					t.Errorf("[%s] source=tomorrow 未注入取数器，却返回空串——"+
						"CLI 会放行一次注定跑不出 B 轨的运行（D4-6 红线 4）", name)
				}

				if gate && (!cfg.API.TomorrowEnabled || !tomorrowKeyConfigured(cfg)) {
					t.Errorf("[%s] 闸门放行了，但 enabled=%v / key齐备=%v",
						name, cfg.API.TomorrowEnabled, tomorrowKeyConfigured(cfg))
				}
			}
		}
	}
}

func TestUnwiredTomorrowAlwaysSpeaksUp(t *testing.T) {
	isolateAPIKeyEnv(t)
	cfg := cfgWithKey(true, "a-real-looking-key")

	if r := TomorrowUnavailableReason(SourceTomorrow, cfg, true); r != "" {
		t.Fatalf("前置条件不成立：配置与密钥齐备时不该有提示，得到 %q", r)
	}

	r := TomorrowUnavailableReason(SourceTomorrow, cfg, false)
	if r == "" {
		t.Fatal("配置与密钥都齐备、但未注入取数器时返回空串——" +
			"这等于宣称 B 轨可用，用户会静默拿到 A 轨报告（D4-6 红线 4）")
	}

	for _, want := range []string{"不是你的配置错", "--source openmeteo"} {
		if !strings.Contains(r, want) {
			t.Errorf("未接线提示应包含 %q，实际为 %q", want, r)
		}
	}
}

func TestEnvKeyNameMatchesClientPackage(t *testing.T) {
	if envTomorrowAPIKey != tomorrow.EnvAPIKey {
		t.Fatalf("环境变量名漂移了：core 用 %q，tomorrow 包用 %q。"+
			"两处必须一致，否则 key 检测会看错变量名",
			envTomorrowAPIKey, tomorrow.EnvAPIKey)
	}
}

func TestParseSourceStrict(t *testing.T) {
	t.Run("合法值与大小写空白不敏感", func(t *testing.T) {
		cases := map[string]Source{
			"openmeteo":   SourceOpenMeteo,
			"OpenMeteo":   SourceOpenMeteo,
			" openmeteo ": SourceOpenMeteo,
			"tomorrow":    SourceTomorrow,
			"Tomorrow":    SourceTomorrow,
			"TOMORROW":    SourceTomorrow,
			"  tomorrow ": SourceTomorrow,
		}
		for in, want := range cases {
			got, err := ParseSource(in)
			if err != nil {
				t.Errorf("ParseSource(%q) 报错：%v", in, err)
				continue
			}
			if got != want {
				t.Errorf("ParseSource(%q) = %q，期望 %q", in, got, want)
			}
		}
	})

	t.Run("空串回落默认_且默认是A轨", func(t *testing.T) {
		got, err := ParseSource("")
		if err != nil {
			t.Fatalf("空串不该报错：%v", err)
		}
		if got != DefaultSource {
			t.Errorf("空串应回落 %q，得到 %q", DefaultSource, got)
		}

		if DefaultSource != SourceOpenMeteo {
			t.Errorf("默认数据源必须是 %q（不耗配额、可判云海），当前为 %q",
				SourceOpenMeteo, DefaultSource)
		}
	})

	t.Run("非法值报错且不回落", func(t *testing.T) {
		for _, bad := range []string{
			"tommorow",
			"open-meteo",
			"openMeteo2",
			"om",
			"none",
			"auto",
		} {
			got, err := ParseSource(bad)
			if err == nil {
				t.Errorf("ParseSource(%q) 必须报错，却回落成了 %q（静默回落会让用户"+
					"拿到不是他要的报告）", bad, got)
				continue
			}
			if got != "" {
				t.Errorf("ParseSource(%q) 报错时应返回零值，得到 %q", bad, got)
			}

			for _, v := range AllSources() {
				if !strings.Contains(err.Error(), string(v)) {
					t.Errorf("ParseSource(%q) 的错误未列出可选值 %q：%v", bad, v, err)
				}
			}
		}
	})
}

func TestSourceLabelsAndHintsAreComplete(t *testing.T) {
	for _, s := range AllSources() {
		if s.Label() == "" {
			t.Errorf("数据源 %q 缺标签", s)
		}
		if s.Hint() == "" {
			t.Errorf("数据源 %q 缺选型提示", s)
		}
	}

	if got := Source("zzz").Label(); got != "zzz" {
		t.Errorf("未知取值的 Label 应回显原串，得到 %q", got)
	}

	if !SourceOpenMeteo.IsDefault() || !Source("").IsDefault() {
		t.Error("openmeteo 与空串都应算默认（等价命令应抑制 --source）")
	}
	if SourceTomorrow.IsDefault() {
		t.Error("tomorrow 不是默认，等价命令必须显式打印 --source tomorrow")
	}

	// AllSources 的展示顺序即菜单与帮助文本顺序：默认轨（openmeteo）打头，
	// 其后依次 B 轨（tomorrow）、C 轨（meteoblue）。新增数据源必须同步更新此断言。
	wantOrder := []Source{SourceOpenMeteo, SourceTomorrow, SourceMeteoblue}
	all := AllSources()
	if len(all) != len(wantOrder) {
		t.Fatalf("AllSources 期望 %d 个（%v），得到 %d 个（%v）",
			len(wantOrder), wantOrder, len(all), all)
	}
	for i := range wantOrder {
		if all[i] != wantOrder[i] {
			t.Errorf("AllSources[%d] 期望 %q，得到 %q（顺序即展示顺序，不可乱）",
				i, wantOrder[i], all[i])
		}
	}
	if all[0] != DefaultSource {
		t.Errorf("AllSources 首元素应为默认轨 %q，得到 %q", DefaultSource, all[0])
	}
}

func TestTomorrowQuotaNoticeCarriesRealNumbers(t *testing.T) {
	notice := TomorrowQuotaNotice()
	for _, want := range []string{"500", "25", "3"} {
		if !strings.Contains(notice, want) {
			t.Errorf("配额提示缺少数字 %s，实际为 %q", want, notice)
		}
	}

	if TomorrowQuotaPerDay != 500 || TomorrowQuotaPerHour != 25 ||
		TomorrowQuotaPerSecond != 3 {
		t.Errorf("免费配额常量被改动：%d/天 %d/小时 %d/秒（原为 500/25/3）",
			TomorrowQuotaPerDay, TomorrowQuotaPerHour, TomorrowQuotaPerSecond)
	}
}
