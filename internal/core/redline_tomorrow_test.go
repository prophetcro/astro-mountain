package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/report"
)

type quotaDownFetcher struct {
	recoverAt time.Time

	calls int
}

func (f *quotaDownFetcher) Name() string { return "fake-quota-down" }

func (f *quotaDownFetcher) FetchSite(context.Context, Site) (
	[]dualtrack.HourInput, string, bool, error) {
	f.calls++
	return nil, dualtrack.DatumAGL, false, nil
}

func (f *quotaDownFetcher) QuotaRecoverAt() time.Time { return f.recoverAt }

type muteFetcher struct{}

func (muteFetcher) Name() string { return "fake-mute" }

func (muteFetcher) FetchSite(context.Context, Site) (
	[]dualtrack.HourInput, string, bool, error) {
	return nil, dualtrack.DatumAGL, false, nil
}

func tomorrowTestConfig() config.Config {
	cfg := config.Default()
	cfg.API.TomorrowEnabled = true
	cfg.API.TomorrowAPIKey = "dummy-key-never-sent"
	return cfg
}

func runTomorrowEngine(t *testing.T, cfg config.Config, fetcher TomorrowFetcher) (
	ExecResult, string) {
	t.Helper()

	stream := redlineStreamFor(t, "clear")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(stream)
	}))
	defer srv.Close()

	client := api.New(cfg.API, false,
		api.WithEndpoint(srv.URL),
		api.WithCache(api.Disabled()),
		api.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
		api.WithSleep(func(time.Duration) {}),
	)

	var out strings.Builder
	e := NewEngine(cfg)
	e.Client = client
	e.TomorrowFetcher = fetcher
	e.Now = func() time.Time { return time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC) }
	e.Logf = func(string, ...any) {}

	res := e.Run(context.Background(), RunParams{
		Peak:       "2026-08-12",
		Days:       1,
		Source:     SourceTomorrow,
		Sites:      redlineOneSite(),
		NoCache:    true,
		ExportCSV:  true,
		ExportJSON: true,
		Stdout:     &out,
		OutDir:     t.TempDir(),
	})
	return res, out.String()
}

var aTrackFingerprints = []string{
	"Open-Meteo 免费 API",
	"Open-Meteo free API",
}

func assertNoATrackLeak(t *testing.T, scenario, text string) {
	t.Helper()
	for _, sig := range aTrackFingerprints {
		if strings.Contains(text, sig) {
			t.Errorf(`[%s] 输出里泄漏了 A 轨特征 %q。

用户显式要的是 Tomorrow.io。此处出现任何 Open-Meteo 署名，他都会把 A 轨结论
当成 B 轨结论读走——D4-6 第 4 条把这定性为本系统最坏的失败模式。

--- 实际输出 ---
%s`, scenario, sig, text)
		}
	}
}

func TestTomorrowUnavailableNeverFallsBackToATrack(t *testing.T) {

	t.Setenv("TOMORROW_API_KEY", "")

	cases := []struct {
		scenario string
		mutate   func(*config.Config)
		attach   bool
		want     string
		wantNot  []string
	}{
		{
			scenario: "总开关关闭",
			mutate: func(c *config.Config) {
				c.API.TomorrowEnabled = false

				c.API.TomorrowAPIKey = "dummy-key-never-used"
			},
			attach:  true,
			want:    "api.tomorrow_enabled",
			wantNot: []string{"未配置 Tomorrow.io API key", "尚未接通"},
		},
		{
			scenario: "key缺失",
			mutate: func(c *config.Config) {
				c.API.TomorrowEnabled = true
				c.API.TomorrowAPIKey = ""
			},
			attach:  true,
			want:    "未配置 Tomorrow.io API key",
			wantNot: []string{"api.tomorrow_enabled 为 false", "尚未接通"},
		},
		{
			scenario: "构建未注入取数器",
			mutate:   func(c *config.Config) {},
			attach:   false,
			want:     "不是你的配置错",
			wantNot:  []string{"未配置 Tomorrow.io API key", "api.tomorrow_enabled 为 false"},
		},
	}

	for _, c := range cases {
		t.Run(c.scenario, func(t *testing.T) {
			cfg := tomorrowTestConfig()
			c.mutate(&cfg)

			var out strings.Builder
			e := NewEngine(cfg)
			e.Now = func() time.Time { return time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC) }
			e.Logf = func(string, ...any) {}
			if c.attach {
				e.TomorrowFetcher = &quotaDownFetcher{}
			}

			res := e.Run(context.Background(), RunParams{
				Peak:   "2026-08-12",
				Days:   1,
				Source: SourceTomorrow,
				Sites:  redlineOneSite(),
				Stdout: &out,
				OutDir: t.TempDir(),
			})

			if res.ExitCode != 2 {
				t.Errorf("[%s] ExitCode = %d，期望 2（参数/配置错误）。errors=%v",
					c.scenario, res.ExitCode, res.Errors)
			}

			if len(res.Rows) != 0 {
				t.Errorf("[%s] 被拒后仍产出了 %d 行 A 轨数据。"+
					"闸门必须在取数**之前**拦下，而不是算完再丢掉——"+
					"调用方拿到的是同一个 ExecResult，它会把这些行当成结论。",
					c.scenario, len(res.Rows))
			}

			if got := out.String(); len(got) != 0 {
				t.Errorf("[%s] stdout 有 %d 字节输出，期望严格 0 字节：\n%q",
					c.scenario, len(got), got)
			}

			if res.CSVPath != "" || res.JSONPath != "" || res.ReportPath != "" {
				t.Errorf("[%s] 被拒后仍落盘了产物：csv=%q json=%q md=%q",
					c.scenario, res.CSVPath, res.JSONPath, res.ReportPath)
			}

			if res.Meta.Source == report.MetaSourceOpenMeteo {
				t.Errorf("[%s] Meta.Source = %q（A 轨署名）。"+
					"被拒的运行不该带着 A 轨身份返回——上层若据此渲染，"+
					"用户就会看到一份署名 Open-Meteo 的东西。",
					c.scenario, res.Meta.Source)
			}

			joined := strings.Join(res.Errors, "\n")
			if !strings.Contains(joined, c.want) {
				t.Errorf("[%s] Errors 没提到本层的专属原因 %q，用户不知道该改什么：\n%s",
					c.scenario, c.want, joined)
			}
			for _, no := range c.wantNot {
				if strings.Contains(joined, no) {
					t.Errorf("[%s] Errors 混进了其它层的原因 %q。"+
						"归错层会把用户引去做无用功（例如为"+
						"「构建没接线」跑去申请密钥）：\n%s", c.scenario, no, joined)
				}
			}

			if !strings.Contains(joined, "不会用 Open-Meteo") {
				t.Errorf("[%s] Errors 没有声明"+
					"「不会用 Open-Meteo（A 轨）替你出一份你没要的报告」：\n%s",
					c.scenario, joined)
			}

			assertNoATrackLeak(t, c.scenario, out.String())
		})
	}
}

func TestTomorrowQuotaExhaustedStaysOnBTrack(t *testing.T) {
	t.Setenv("TOMORROW_API_KEY", "")

	recover := time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)
	fetcher := &quotaDownFetcher{recoverAt: recover}
	res, out := runTomorrowEngine(t, tomorrowTestConfig(), fetcher)

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d，期望 0。配额耗尽有明确归因与恢复时间，"+
			"是一份完整的 B 轨结论，不该让脚本判成失败。errors=%v",
			res.ExitCode, res.Errors)
	}

	if fetcher.calls == 0 {
		t.Fatal("B 轨取数器一次都没被调用。闸门放行之后 Engine.Run 却没消费 B 轨，" +
			"这正是 2026-08-07 第二次现场的形状：exit=0、出报告、内容全是 A 轨。")
	}

	if res.Meta.Source != report.MetaSourceTomorrow {
		t.Errorf("Meta.Source = %q，期望 %q。报告署名必须是**本轮真正跑的那条轨**——"+
			"这是用户判断自己在看什么的唯一依据。",
			res.Meta.Source, report.MetaSourceTomorrow)
	}
	if !strings.Contains(out, report.TomorrowTrackLabel) {
		t.Errorf("终端输出里没有 B 轨署名 %q。结构化字段对了但表头没跟上，"+
			"用户看到的仍是一份来路不明的报告。\n--- 输出 ---\n%s",
			report.TomorrowTrackLabel, out)
	}

	if len(res.Tomorrow) == 0 {
		t.Fatal("res.Tomorrow 为空：B 轨结果没有被装进 ExecResult，" +
			"报告层拿不到任何东西可渲染。")
	}
	for _, tr := range res.Tomorrow {
		if !tr.QuotaExhausted {
			t.Errorf("[%s] TrackResult.QuotaExhausted = false，"+
				"但取数器明确回报了 quotaOK=false。这个位丢了，"+
				"报告就说不出"+"「本轮没额度」"+"这句话。", tr.SiteID)
		}
		if tr.Active {
			t.Errorf("[%s] TrackResult.Active = true。一次请求都没发出去的轨"+
				"不该自称活着——总览会把它算进"+"「已覆盖点位」"+"里。", tr.SiteID)
		}
		if len(tr.Rows) == 0 {
			t.Errorf("[%s] 零行结果。配额耗尽时仍要按 A 轨时间轴铺满 NODATA 行，"+
				"否则用户看到的是一张空表，分不清"+
				"「没额度」"+"和"+"「今晚本来就没时次」"+"。", tr.SiteID)
			continue
		}
		if got, want := tr.CountByReason(dualtrack.RoundQuotaDown), len(tr.Rows); got != want {
			t.Errorf("[%s] 归因为 ROUND_QUOTA_DOWN 的行数 = %d，期望全部 %d 行。\n"+
				"归错类的代价很具体：报成"+"「取数失败」"+"用户会立刻重试并再撞一次墙；"+
				"报成"+"「超预报窗」"+"用户会以为明天也没有，直接放弃。", tr.SiteID, got, want)
		}
	}

	first := res.Tomorrow[0]
	if first.NextAvailable == nil {
		t.Error("NextAvailable 为 nil，但取数器实现了 TomorrowQuotaReporter。" +
			"恢复时刻只有配额层知道，dualtrack.Assemble 填不了；" +
			"core 又不许 import api/tomorrow，中立扩展接口就是唯一通路——" +
			"它没接上，用户就只能看到" + "「不知道什么时候恢复」" + "。")
	} else if !first.NextAvailable.Equal(recover) {
		t.Errorf("NextAvailable = %v，期望 %v", *first.NextAvailable, recover)
	}

	if !strings.Contains(out, model.RATING_NODATA) {
		t.Errorf("终端输出里没有 %q。NODATA 行必须打印出来而不是被过滤掉——"+
			"B 轨的每一条 NODATA 都带着归因，隐掉就等于把"+
			"「为什么没结论」"+"这个最有用的信息一起扔了。\n--- 输出 ---\n%s",
			model.RATING_NODATA, out)
	}
	quotaTag := "[" + dualtrack.NoDataReasonLabels[dualtrack.RoundQuotaDown] + "]"
	if !strings.Contains(out, quotaTag) {
		t.Errorf("终端输出里没有归因标签 %q。只写"+"「无数据」"+"而不说为什么，"+
			"用户无从判断该等配额、该改配置、还是该换个日期。\n--- 输出 ---\n%s",
			quotaTag, out)
	}

	assertNoATrackLeak(t, "配额耗尽", out)

	assertBTrackArtifacts(t, res)
}

func TestTomorrowQuotaReporterOptionalDegradesHonestly(t *testing.T) {
	t.Setenv("TOMORROW_API_KEY", "")

	res, _ := runTomorrowEngine(t, tomorrowTestConfig(), muteFetcher{})
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d，期望 0。errors=%v", res.ExitCode, res.Errors)
	}
	if len(res.Tomorrow) == 0 {
		t.Fatal("res.Tomorrow 为空")
	}
	if at := res.Tomorrow[0].NextAvailable; at != nil {
		t.Errorf("取数器没实现 TomorrowQuotaReporter，NextAvailable 却是 %v。"+
			"缺了这个能力的正确表现是 nil（"+"「恢复时间未知」"+"），"+
			"填一个编造的时刻会让用户干等。", *at)
	}
}

func assertBTrackArtifacts(t *testing.T, res ExecResult) {
	t.Helper()

	for _, a := range []struct {
		name string
		path string

		want string

		wantNot string
	}{
		{
			name: "CSV",
			path: res.CSVPath,

			want: TomorrowFieldLabel(t, "no_data_reason"),

			wantNot: "云顶",
		},
		{
			name:    "JSON",
			path:    res.JSONPath,
			want:    `"no_data_reason"`,
			wantNot: `"cloud_top`,
		},
		{
			name:    "Markdown",
			path:    res.ReportPath,
			want:    report.TomorrowTrackLabel,
			wantNot: "Open-Meteo 免费 API",
		},
	} {
		if a.path == "" {
			t.Errorf("%s 未落盘（路径为空）", a.name)
			continue
		}
		data, err := os.ReadFile(a.path)
		if err != nil {
			t.Errorf("读取 %s 产物 %s 失败：%v", a.name, a.path, err)
			continue
		}
		text := string(data)
		if !strings.Contains(text, a.want) {
			t.Errorf("%s 产物里没有 B 轨特征 %q——它很可能走了 A 轨导出分支。\n路径：%s",
				a.name, a.want, a.path)
		}
		if strings.Contains(text, a.wantNot) {
			t.Errorf("%s 产物里出现了 A 轨特征 %q。用户显式选的是 B 轨，"+
				"这份文件会被转发、归档、喂给下游脚本，届时没有任何线索"+
				"说明它其实是 Open-Meteo 算的。\n路径：%s", a.name, a.wantNot, a.path)
		}
	}
}

func TomorrowFieldLabel(t *testing.T, field string) string {
	t.Helper()
	label, ok := report.TomorrowFieldLabels[field]
	if !ok || label == "" {
		t.Fatalf("report.TomorrowFieldLabels 里没有字段 %q 的标签——"+
			"字段被改名或删除了，本用例的断言已经失效，请同步更新。", field)
	}
	return label
}
