package cli

import (
	"bytes"
	"strings"
	"testing"
)

const aTrackMarker = "Open-Meteo 免费 API"

func TestSourceTomorrow_NeverSilentlyFallsBackToOpenMeteo(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		cfgKey  string
		envKey  string
	}{
		{"密钥缺失_出厂默认状态", true, "", ""},
		{"总开关关闭", false, "", ""},
		{"密钥在配置文件里", true, "key-from-config", ""},
		{"密钥在环境变量里", true, "", "key-from-env"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {

			t.Setenv("TOMORROW_API_KEY", c.envKey)

			args := []string{
				"--peak", "2026-08-12",
				"--source", "tomorrow",
				"--no-report", "--no-douyin",
				"--config", writeTempConfig(t, c.enabled, c.cfgKey),
			}

			var out, errOut bytes.Buffer
			code := MainWith(args, "dev", &out, &errOut, false)

			if code == ExitOK {
				t.Fatalf("退出码 0——脚本会认为这轮成功并把 A 轨数据当成 B 轨用。\n"+
					"stdout：%s\nstderr：%s", out.String(), errOut.String())
			}

			if code != ExitUsage {
				t.Errorf("退出码应为 %d（参数/配置错误），实际 %d", ExitUsage, code)
			}

			if strings.Contains(out.String(), aTrackMarker) {
				t.Errorf("用户选了 B 轨，stdout 却出现了 A 轨报告特征串 %q（D4-6 红线 4）：\n%s",
					aTrackMarker, out.String())
			}
			if strings.TrimSpace(out.String()) != "" {
				t.Errorf("中止路径不该往 stdout 写任何东西，实际：%s", out.String())
			}

			stderr := errOut.String()
			for _, want := range []string{"--source tomorrow", "--source openmeteo"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr 应提到 %q，实际：%s", want, stderr)
				}
			}
		})
	}
}

func TestSourceTomorrow_MissingKeyTellsUserHowToConfigure(t *testing.T) {
	t.Setenv("TOMORROW_API_KEY", "")

	args := []string{
		"--peak", "2026-08-12",
		"--source", "tomorrow",
		"--no-report", "--no-douyin",
		"--config", writeTempConfig(t, true, ""),
	}

	var out, errOut bytes.Buffer
	if code := MainWith(args, "dev", &out, &errOut, false); code != ExitUsage {
		t.Fatalf("退出码应为 %d，实际 %d", ExitUsage, code)
	}

	stderr := errOut.String()
	for _, want := range []string{
		"TOMORROW_API_KEY",
		"api.tomorrow_api_key",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("密钥缺失的提示应包含 %q，否则用户只能去翻源码。实际：%s",
				want, stderr)
		}
	}
}
