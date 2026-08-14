package tomorrow

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

var testSite = model.Site{Name: "牵牛岗", Lat: 30.026, Lon: 119.007, Alt: 1489.9}

const okBody = `{"timelines":{"hourly":[
  {"time":"2026-01-01T00:00:00Z","values":{"cloudBase":1.2,"cloudCeiling":1.6,"cloudCover":75}},
  {"time":"2026-01-01T01:00:00Z","values":{"cloudBase":1.3,"cloudCeiling":1.7,"cloudCover":80}},
  {"time":"2026-01-01T02:00:00Z","values":{"cloudBase":1.4,"cloudCeiling":1.8,"cloudCover":85}}
]}}`

func newTestClient(t *testing.T, srv *httptest.Server, opts ...Option) *Client {
	t.Helper()
	base := []Option{
		WithAPIKey("SECRETKEY", KeySourceEnv),
		WithHTTPClient(srv.Client()),
		WithSleep(func(time.Duration) {}),
	}
	cfg := config.APIConfig{
		TomorrowEndpoint:      srv.URL,
		TomorrowCloudBaseUnit: "km",
		Retries:               3,
		BackoffFactor:         0.01,
	}
	return New(cfg, false, append(base, opts...)...)
}

func countingServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		h(w, r)
	}))
	return srv, &hits
}

func TestNewWithoutKeyReturnsUsableClient(t *testing.T) {
	srv, hits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {})
	defer srv.Close()

	c := New(
		config.APIConfig{TomorrowEndpoint: srv.URL},
		false,
		WithHTTPClient(srv.Client()),
		WithGetenv(func(string) string { return "" }),
	)
	if c == nil {
		t.Fatal("无 key 时 New 返回了 nil —— 降级是正常路径，不该逼调用方做 nil 检查")
	}
	if c.HasKey() {
		t.Error("HasKey() 应为 false")
	}
	if c.KeySource() != KeySourceNone {
		t.Errorf("KeySource() = %q，期望 none", c.KeySource())
	}
	if _, err := c.FetchSite(context.Background(), testSite); !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("FetchSite err = %v，期望 ErrNoAPIKey", err)
	}
	if *hits != 0 {
		t.Errorf("无 key 却发了 %d 次请求，期望 0 次", *hits)
	}
}

func TestNewPrefersEnvKeyOverConfig(t *testing.T) {
	c := New(
		config.APIConfig{TomorrowAPIKey: "FROM_CONFIG"},
		false,
		WithGetenv(func(k string) string {
			if k == EnvAPIKey {
				return "FROM_ENV"
			}
			return ""
		}),
	)
	if c.KeySource() != KeySourceEnv {
		t.Errorf("KeySource() = %q，期望 env", c.KeySource())
	}
	if c.apiKey != "FROM_ENV" {
		t.Errorf("apiKey = %q，期望 FROM_ENV", c.apiKey)
	}
}

func TestNewFallsBackToConfigKey(t *testing.T) {
	c := New(
		config.APIConfig{TomorrowAPIKey: "FROM_CONFIG"},
		false,
		WithGetenv(func(string) string { return "" }),
	)
	if c.KeySource() != KeySourceConfig {
		t.Errorf("KeySource() = %q，期望 config", c.KeySource())
	}
}

func TestFetchSiteConvertsToMSL(t *testing.T) {
	srv, hits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(okBody))
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	res, err := c.FetchSite(context.Background(), testSite)
	if err != nil {
		t.Fatalf("FetchSite 失败：%v", err)
	}
	if *hits != 1 {
		t.Errorf("发了 %d 次请求，期望 1 次", *hits)
	}
	if len(res.Samples) != 3 {
		t.Fatalf("样本数 = %d，期望 3", len(res.Samples))
	}
	if res.ResolvedUnit != UnitKilometer {
		t.Errorf("ResolvedUnit = %q，期望 km", res.ResolvedUnit)
	}
	if res.UnitGuessed {
		t.Error("显式配了 km，UnitGuessed 不该为 true")
	}

	if got := res.CloudBaseMSL[0]; !got.Valid || math.Abs(got.V-2689.9) > 1e-6 {
		t.Errorf("CloudBaseMSL[0] = %+v，期望 2689.9", got)
	}
}

func TestFetchSiteWithMSLDatumSkipsSiteAlt(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(okBody))
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	c.Datum = DatumMSL
	res, err := c.FetchSite(context.Background(), testSite)
	if err != nil {
		t.Fatalf("FetchSite 失败：%v", err)
	}

	if got := res.CloudBaseMSL[0]; !got.Valid || math.Abs(got.V-1200) > 1e-6 {
		t.Errorf("CloudBaseMSL[0] = %+v，期望 1200", got)
	}
}

func TestFetchSiteAutoUnitFlagsGuessed(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(okBody))
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	c.Unit = UnitAuto
	res, err := c.FetchSite(context.Background(), testSite)
	if err != nil {
		t.Fatalf("FetchSite 失败：%v", err)
	}
	if !res.UnitGuessed {
		t.Error("auto 模式下 UnitGuessed 必须为 true，否则报告不会打 WARN 脚注")
	}
	if res.ResolvedUnit != UnitKilometer {
		t.Errorf("ResolvedUnit = %q，期望启发式判出 km", res.ResolvedUnit)
	}
}

func TestNullCloudBaseStaysMissing(t *testing.T) {
	const body = `{"timelines":{"hourly":[
	  {"time":"2026-01-01T00:00:00Z","values":{"cloudBase":null,"cloudCover":0}},
	  {"time":"2026-01-01T01:00:00Z","values":{"cloudCover":10}},
	  {"time":"2026-01-01T02:00:00Z","values":{"cloudBase":1.2}}
	]}}`
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	defer srv.Close()

	res, err := newTestClient(t, srv).FetchSite(context.Background(), testSite)
	if err != nil {
		t.Fatalf("FetchSite 失败：%v", err)
	}

	if res.Samples[0].CloudBaseRaw.Valid {
		t.Error("cloudBase:null 被读成了有效值——这会让缺测退化成「云贴地」的最坏结论")
	}
	if res.CloudBaseMSL[0].Valid {
		t.Error("null 云底换算后仍应缺测")
	}

	if res.Samples[1].CloudBaseRaw.Valid {
		t.Error("缺少 cloudBase 键时应视为缺测")
	}

	if !res.Samples[2].CloudBaseRaw.Valid {
		t.Error("有值的 cloudBase 反而被判缺测")
	}

	if !res.Samples[0].CloudCover.Valid || res.Samples[0].CloudCover.V != 0 {
		t.Errorf("cloudCover:0 应为有效的 0，实际 %+v", res.Samples[0].CloudCover)
	}
}

func TestFetchSiteUnauthorizedNotRetried(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv, hits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"message":"nope"}`))
		})
		_, err := newTestClient(t, srv).FetchSite(context.Background(), testSite)
		srv.Close()

		if !errors.Is(err, ErrUnauthed) {
			t.Errorf("HTTP %d 的 err = %v，期望 ErrUnauthed", code, err)
		}
		if *hits != 1 {
			t.Errorf("HTTP %d 重试了 %d 次，鉴权失败重试没有意义", code, *hits)
		}
	}
}

func TestFetchSiteRateLimitNotRetried(t *testing.T) {
	srv, hits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer srv.Close()

	_, err := newTestClient(t, srv).FetchSite(context.Background(), testSite)
	if !errors.Is(err, ErrRateLimit) {
		t.Fatalf("err = %v，期望 ErrRateLimit", err)
	}
	if *hits != 1 {
		t.Fatalf("429 被重试了 %d 次，期望恰好 1 次（不重试）", *hits)
	}
}

func TestFetchSiteRetriesServerErrorUpToRetries(t *testing.T) {
	srv, hits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := c.FetchSite(context.Background(), testSite); err == nil {
		t.Fatal("恒 500 时期望返回错误")
	}
	if *hits != 3 {
		t.Fatalf("5xx 请求了 %d 次，期望等于 Retries=3", *hits)
	}
}

func TestFetchSiteRecoversAfterTransient500(t *testing.T) {
	srv, hits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer srv.Close()

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if *hits == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(okBody))
	})

	res, err := newTestClient(t, srv).FetchSite(context.Background(), testSite)
	if err != nil {
		t.Fatalf("瞬时 500 后应当重试成功，实际：%v", err)
	}
	if len(res.Samples) != 3 {
		t.Errorf("样本数 = %d，期望 3", len(res.Samples))
	}
	if *hits != 2 {
		t.Errorf("请求了 %d 次，期望 2 次（1 次失败 + 1 次成功）", *hits)
	}
}

func TestFetchSiteHonoursCanceledContext(t *testing.T) {
	srv, hits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(okBody))
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := newTestClient(t, srv).FetchSite(ctx, testSite); err == nil {
		t.Fatal("ctx 已取消时期望返回错误")
	}
	if *hits != 0 {
		t.Errorf("ctx 已取消却发了 %d 次请求", *hits)
	}
}

func TestFetchSiteTruncatesOversizedBody(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"timelines":{"hourly":[{"time":"2026-01-01T00:00:00Z","values":{"note":"`))
		chunk := strings.Repeat("A", 64*1024)
		for written := 0; written < maxBodyBytes+128*1024; written += len(chunk) {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	})
	defer srv.Close()

	if _, err := newTestClient(t, srv).FetchSite(context.Background(), testSite); err == nil {
		t.Fatal("超大响应体应当因截断而解析失败，而不是被完整读进内存")
	}
}

func TestCacheHitsAcrossDifferentAPIKeys(t *testing.T) {
	srv, hits := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(okBody))
	})
	defer srv.Close()

	cache := api.NewCache(t.TempDir(), time.Hour)
	mk := func(key string) *Client {
		return New(
			config.APIConfig{
				TomorrowEndpoint:      srv.URL,
				TomorrowCloudBaseUnit: "km",
				Retries:               1,
			},
			false,
			WithAPIKey(key, KeySourceEnv),
			WithHTTPClient(srv.Client()),
			WithCache(cache),
			WithSleep(func(time.Duration) {}),
		)
	}

	if _, err := mk("KEY_A").FetchSite(context.Background(), testSite); err != nil {
		t.Fatalf("第一次取数失败：%v", err)
	}
	if *hits != 1 {
		t.Fatalf("第一次请求数 = %d，期望 1", *hits)
	}

	if _, err := mk("KEY_B_TOTALLY_DIFFERENT").FetchSite(context.Background(), testSite); err != nil {
		t.Fatalf("第二次取数失败：%v", err)
	}
	if *hits != 1 {
		t.Errorf("换 key 后又发了请求（累计 %d 次），说明缓存键没剥掉 apikey", *hits)
	}
}

func TestCacheURLHasNoAPIKey(t *testing.T) {
	c := New(
		config.APIConfig{TomorrowEndpoint: "https://x.test/v4/weather/forecast"},
		false,
		WithAPIKey("SUPERSECRET", KeySourceEnv),
	)
	full := c.BuildURL(testSite)
	if !strings.Contains(full, "SUPERSECRET") {
		t.Fatal("BuildURL 应当带上真实 key（它是真正发出去的地址）")
	}
	cacheKey := c.cacheURL(testSite)
	if strings.Contains(cacheKey, "SUPERSECRET") {
		t.Errorf("cacheURL 泄漏了密钥：%s", cacheKey)
	}
	if strings.Contains(cacheKey, "apikey") {
		t.Errorf("cacheURL 仍保留 apikey 参数：%s", cacheKey)
	}

	if !strings.Contains(cacheKey, "location") {
		t.Errorf("cacheURL 丢掉了 location，两个点位会串缓存：%s", cacheKey)
	}
	if strings.Contains(RedactURL(full), "SUPERSECRET") {
		t.Error("RedactURL 泄漏了密钥")
	}
}

func TestErrorsNeverLeakAPIKey(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)

		_, _ = w.Write([]byte(`{"message":"bad request for apikey=SECRETKEY"}`))
	})
	defer srv.Close()

	_, err := newTestClient(t, srv).FetchSite(context.Background(), testSite)
	if err == nil {
		t.Fatal("HTTP 400 期望返回错误")
	}
	if strings.Contains(err.Error(), "SECRETKEY") {
		t.Fatalf("错误信息泄漏了明文 key：%v", err)
	}
	if !strings.Contains(err.Error(), redactedMark) {
		t.Errorf("错误信息里没有脱敏标记，脱敏可能没生效：%v", err)
	}
}

func TestBuildURLCarriesRequiredParams(t *testing.T) {
	c := New(
		config.APIConfig{},
		false,
		WithAPIKey("K", KeySourceEnv),
	)
	if c.Endpoint != DefaultEndpoint {
		t.Errorf("端点为空时应回落到 DefaultEndpoint，实际 %q", c.Endpoint)
	}
	u := c.BuildURL(testSite)
	for _, want := range []string{
		"location=30.026%2C119.007",
		"timesteps=1h",
		"units=metric",
		"apikey=K",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("BuildURL 缺少 %q：%s", want, u)
		}
	}
}

func TestNewToleratesBadUnitAndDatum(t *testing.T) {
	var logs []string
	c := New(
		config.APIConfig{
			TomorrowCloudBaseUnit:  "meters",
			TomorrowCloudBaseDatum: "ground",
		},
		false,
		WithAPIKey("K", KeySourceEnv),
		WithLogger(func(f string, a ...any) { logs = append(logs, f) }),
	)
	if c.Unit != UnitAuto {
		t.Errorf("非法单位应退回 auto，实际 %q", c.Unit)
	}
	if c.Datum != DatumAGL {
		t.Errorf("非法基准应退回 agl，实际 %q", c.Datum)
	}
	if len(logs) != 2 {
		t.Errorf("应当为两处非法配置各留一条警告，实际 %d 条", len(logs))
	}
}
