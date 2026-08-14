package tomorrow

import (
	"strings"
	"testing"
)

func TestResolveAPIKey(t *testing.T) {
	cases := []struct {
		name         string
		env          string
		cfg          string
		wantResolved string
		wantSource   KeySource
		wantOK       bool
	}{
		{"env优先", "ENVKEY", "CFGKEY", "ENVKEY", KeySourceEnv, true},
		{"env为空用cfg", "", "CFGKEY", "CFGKEY", KeySourceConfig, true},
		{"env为纯空白视为空", "   ", "CFGKEY", "CFGKEY", KeySourceConfig, true},
		{"两者皆空", "", "", "", KeySourceNone, false},
		{"cfg为空env有", "ENVKEY", "", "ENVKEY", KeySourceEnv, true},
		{"两者都为空白", "  ", "  ", "", KeySourceNone, false},

		{"env带首尾空白：判空与取值同口径", "  ENVKEY  ", "CFGKEY", "ENVKEY", KeySourceEnv, true},
		{"cfg带换行同样去空白", "", "CFGKEY\n", "CFGKEY", KeySourceConfig, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, src, ok := ResolveAPIKey(tc.env, tc.cfg)
			if got != tc.wantResolved || src != tc.wantSource || ok != tc.wantOK {
				t.Fatalf("ResolveAPIKey(%q,%q) = (%q,%q,%v)，期望 (%q,%q,%v)",
					tc.env, tc.cfg, got, src, ok, tc.wantResolved, tc.wantSource, tc.wantOK)
			}
		})
	}
}

func TestKeySourceDescribeNeverLeaksKey(t *testing.T) {

	for _, s := range []KeySource{KeySourceEnv, KeySourceConfig, KeySourceNone} {
		if d := s.Describe(); d == "" {
			t.Errorf("KeySource(%q).Describe() 不该为空", s)
		}
	}
	if !strings.Contains(KeySourceEnv.Describe(), EnvAPIKey) {
		t.Error("env 来源的说明里应当点出环境变量名，方便用户知道去哪儿改")
	}
}

func TestRedactURL(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantNoLeak  string
		wantContain string
	}{
		{"apikey被脱敏",
			"https://api.tomorrow.io/v4/weather/forecast?location=1,2&apikey=SECRET123",
			"SECRET123", "apikey=" + redactedMark},
		{"api_key被脱敏",
			"https://x.test/forecast?api_key=TOPSECRET&timesteps=1h",
			"TOPSECRET", "api_key=" + redactedMark},
		{"key被脱敏",
			"https://x.test/f?key=K123&location=3,4",
			"K123", "key=" + redactedMark},
		{"token被脱敏",
			"https://x.test/f?token=T999",
			"T999", "token=" + redactedMark},
		{"参数名大小写不敏感",
			"https://x.test/f?ApiKey=MiXeD123",
			"MiXeD123", redactedMark},
		{"无密钥原样返回",
			"https://x.test/f?location=3,4&timesteps=1h",
			"", "location=3,4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactURL(tc.raw)
			if tc.wantNoLeak != "" && strings.Contains(got, tc.wantNoLeak) {
				t.Fatalf("RedactURL 泄漏了明文密钥：%q 中仍包含 %q", got, tc.wantNoLeak)
			}
			if tc.wantContain != "" && !strings.Contains(got, tc.wantContain) {
				t.Fatalf("RedactURL 结果缺少预期子串：%q 中应包含 %q", got, tc.wantContain)
			}
		})
	}

	if got := RedactURL("://not a url?apikey=LEAKME"); got != redactedMark {
		t.Fatalf("非法 URL 应退化为 %q，实际：%q", redactedMark, got)
	}
}

func TestRedactURLPreservesOtherParamsVerbatim(t *testing.T) {
	raw := "https://x.test/f?location=30.026,119.007&timesteps=1h&apikey=SECRET"
	got := RedactURL(raw)
	if !strings.Contains(got, "location=30.026,119.007") {
		t.Errorf("非密钥参数被重新编码了：%q", got)
	}
	if !strings.Contains(got, "timesteps=1h") {
		t.Errorf("非密钥参数丢失：%q", got)
	}

	if strings.Contains(got, "%2A") {
		t.Errorf("脱敏标记被转义成了 %%2A，日志里没法一眼认出：%q", got)
	}
}

func TestStripAPIKey(t *testing.T) {
	raw := "https://api.tomorrow.io/v4/weather/forecast?location=1,2&timesteps=1h&apikey=SECRET123"
	got := stripAPIKey(raw)
	if strings.Contains(got, "SECRET123") {
		t.Fatalf("stripAPIKey 泄漏了明文密钥：%q", got)
	}
	if strings.Contains(got, "apikey") {
		t.Fatalf("stripAPIKey 应整对移除 apikey 参数，实际：%q", got)
	}

	if !strings.Contains(got, "location=1,2") || !strings.Contains(got, "timesteps=1h") {
		t.Fatalf("stripAPIKey 改动了非密钥参数，实际：%q", got)
	}
}

func TestStripAPIKeyKeepsSitesDistinct(t *testing.T) {
	a := stripAPIKey("https://x.test/f?location=30.0,119.0&apikey=K")
	b := stripAPIKey("https://x.test/f?location=28.8,120.9&apikey=K")
	if a == b {
		t.Fatalf("不同点位剥 key 后 URL 相同，会串缓存：%q", a)
	}
}

func TestStripAPIKeyOnlySecretParam(t *testing.T) {
	got := stripAPIKey("https://x.test/f?apikey=SECRET")
	if got != "https://x.test/f" {
		t.Fatalf("stripAPIKey = %q，期望 %q", got, "https://x.test/f")
	}
}

func TestRewriteQueryNoQueryString(t *testing.T) {
	raw := "https://x.test/forecast"
	if got := stripAPIKey(raw); got != raw {
		t.Fatalf("无查询串时应原样返回，实际：%q", got)
	}
	if got := RedactURL(raw); got != raw {
		t.Fatalf("无查询串时应原样返回，实际：%q", got)
	}
}
