package core

import (
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
)

func cfgWithMeteoblue(enabled bool, key string) config.Config {
	c := config.Default()
	c.API.MeteoblueEnabled = enabled
	c.API.MeteoblueAPIKey = key
	return c
}

func isolateMeteoblueKeyEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envMeteoblueAPIKey, "")
}

func TestOpenMeteoNeverTouchesMeteoblue(t *testing.T) {
	notMeteoblue := []Source{
		SourceOpenMeteo,
		SourceTomorrow,
		"",
		"openmeteo",
		"tomorrow",
	}

	for _, enabled := range []bool{true, false} {
		cfg := cfgWithMeteoblue(enabled, "k")
		for _, src := range notMeteoblue {
			if UseMeteoblue(src, cfg) {
				t.Errorf("source=%q meteoblue_enabled=%v：UseMeteoblue 返回 true，"+
					"说明配置判断跑到了 Source 前面", src, enabled)
			}
			for _, wired := range []bool{true, false} {
				if r := MeteoblueUnavailableReason(src, cfg, wired); r != "" {
					t.Errorf("source=%q wired=%v：不该产出 C 轨不可用提示，得到 %q",
						src, wired, r)
				}
			}
		}
	}
}

func TestUseMeteoblueShortCircuitsBeforeConfig(t *testing.T) {
	on, off := cfgWithMeteoblue(true, "k"), cfgWithMeteoblue(false, "k")

	if UseMeteoblue(SourceOpenMeteo, on) || UseMeteoblue(SourceOpenMeteo, off) {
		t.Fatal("openmeteo 下 UseMeteoblue 必须无条件 false")
	}
	if UseMeteoblue(SourceTomorrow, on) || UseMeteoblue(SourceTomorrow, off) {
		t.Fatal("tomorrow 下 UseMeteoblue 必须无条件 false")
	}

	if !UseMeteoblue(SourceMeteoblue, on) {
		t.Error("meteoblue + meteoblue_enabled=true 应放行")
	}
	if UseMeteoblue(SourceMeteoblue, off) {
		t.Error("meteoblue + meteoblue_enabled=false 必须拦下")
	}
}

func TestMeteoblueUnavailableReasonSpeaksUp(t *testing.T) {
	isolateMeteoblueKeyEnv(t)

	r := MeteoblueUnavailableReason(SourceMeteoblue, cfgWithMeteoblue(false, ""), true)
	if r == "" {
		t.Fatal("选了 C 轨但配置关着，必须给出原因，不能静默降级")
	}
	if !strings.Contains(r, "api.meteoblue_enabled") {
		t.Errorf("提示应点名配置项 api.meteoblue_enabled，实际为 %q", r)
	}
	if !strings.Contains(r, "--source openmeteo") {
		t.Errorf("提示应给出退路 --source openmeteo，实际为 %q", r)
	}
}

func TestMeteoblueUnavailableReasonCoversMissingKey(t *testing.T) {
	t.Run("开关开着但没密钥_必须出声且说清怎么配", func(t *testing.T) {
		isolateMeteoblueKeyEnv(t)

		r := MeteoblueUnavailableReason(SourceMeteoblue, cfgWithMeteoblue(true, ""), true)
		if r == "" {
			t.Fatal("meteoblue_enabled=true 但 key 为空时返回了空串——" +
				"这等于宣称 C 轨可用，用户会静默拿到 A 轨报告")
		}
		for _, want := range []string{
			envMeteoblueAPIKey,
			"api.meteoblue_api_key",
			"--source openmeteo",
		} {
			if !strings.Contains(r, want) {
				t.Errorf("密钥缺失提示应包含 %q（用户才知道怎么修），实际为 %q", want, r)
			}
		}
	})

	t.Run("密钥来自环境变量_不该再被判成没配", func(t *testing.T) {
		t.Setenv(envMeteoblueAPIKey, "key-from-env")

		r := MeteoblueUnavailableReason(SourceMeteoblue, cfgWithMeteoblue(true, ""), true)
		if strings.Contains(r, "未配置 Meteoblue API key") {
			t.Errorf("env 里已有 key，却仍报「未配置密钥」：%q", r)
		}
		if !UseMeteoblue(SourceMeteoblue, cfgWithMeteoblue(true, "")) {
			t.Error("env 里已有 key，闸门却关着——用 env 配 key 是文档推荐的主路径")
		}
	})

	t.Run("纯空白的密钥等于没配", func(t *testing.T) {
		t.Setenv(envMeteoblueAPIKey, "   ")

		r := MeteoblueUnavailableReason(SourceMeteoblue, cfgWithMeteoblue(true, "\t\n "), true)
		if !strings.Contains(r, envMeteoblueAPIKey) {
			t.Errorf("全空白的 key 应视同没配并给出配置指引，实际为 %q", r)
		}
	})

	t.Run("配置齐备但链路未接通_归因为构建问题", func(t *testing.T) {
		t.Setenv(envMeteoblueAPIKey, "k")
		r := MeteoblueUnavailableReason(SourceMeteoblue, cfgWithMeteoblue(true, "k"), false)
		if r == "" {
			t.Fatal("已配置但 Deliverable=false 时必须给出原因")
		}
		if !strings.Contains(r, "尚未接通") {
			t.Errorf("应明确告知是构建/接线问题而非用户配置错，实际为 %q", r)
		}
		if strings.Contains(r, envMeteoblueAPIKey) {
			t.Errorf("链路未接通不该再怪密钥，实际为 %q", r)
		}
	})
}
