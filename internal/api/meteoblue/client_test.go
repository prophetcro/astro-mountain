package meteoblue

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// cannedResponse 是一段最小但可被 ParseResponse 解码的 Meteoblue 1h 响应，
// 字段名严格对照 openapi.yml（basic-1h + clouds-1h 合并包），含一个落在夜间窗口的
// 时刻（timeformat=iso8601 → RFC3339 带 +08:00 偏移），便于端到端验证
// FetchSite 的「取数→解析→评估」全链。
const cannedResponse = `{
  "data_1h": {
    "time": ["2026-08-12T22:00:00+08:00"],
    "temperature": [15.0],
    "relativehumidity": [85.0],
    "precipitation": [0.0],
    "precipitation_probability": [0.0],
    "windspeed": [4.0],
    "visibility": [10000.0],
    "totalcloudcover": [30.0],
    "lowclouds": [20.0],
    "midclouds": [10.0],
    "highclouds": [5.0],
    "fog_probability": [10.0]
  }
}`

func TestFetchSiteNoKeyReturnsErrNoAPIKey(t *testing.T) {
	// 关键红线：无 key 时 C 轨必须明确失败，绝不回落 A 轨。
	cfg := config.Default()
	cfg.API.MeteoblueEnabled = true
	cfg.API.MeteoblueAPIKey = ""
	t.Setenv(EnvAPIKey, "")

	c := New(cfg)
	_, err := c.FetchSite(context.Background(), testSite(),
		time.Now(), time.Now().Add(48*time.Hour), map[string]bool{"2026-08-12": true})
	if err == nil {
		t.Fatal("无 key 时 FetchSite 应返回错误，却返回 nil")
	}
	if !strings.Contains(err.Error(), "未配置 API key") {
		t.Errorf("错误信息应说明未配置 key，得到 %q", err.Error())
	}
}

// TestBuildURLUsesCorrectUnitsAndPackages 是 OpenAPI 契约回归测试：
// 此前 windSpeedUnit="ms" 导致所有站点 HTTP 400（枚举只认 m/s），必须钉死。
func TestBuildURLUsesCorrectUnitsAndPackages(t *testing.T) {
	cfg := config.Default()
	cfg.API.MeteoblueEnabled = true
	cfg.API.MeteoblueAPIKey = "test-key"
	c := New(cfg)

	site := testSite()
	u := c.buildURL(site, 3)

	if !strings.Contains(u, "basic-3h,clouds-3h") {
		t.Errorf("包组合应为 basic-3h,clouds-3h（免费档默认，clouds-1h 不在免费清单），URL=%s", u)
	}
	if !strings.Contains(u, "windSpeedUnit=m%2Fs") {
		// url.Values 会编码 '/' 为 %2F；服务端解码后仍是 m/s。
		t.Errorf("windSpeedUnit 应编码为 m/s（m%%2Fs），URL=%s", u)
	}
	if !strings.Contains(u, "forecastDays=3") {
		t.Errorf("forecastDays 应为 3，URL=%s", u)
	}
	if !strings.Contains(u, "timeformat=iso8601") {
		t.Errorf("timeformat 应为 iso8601，URL=%s", u)
	}
}

func TestFetchSiteEndToEndViaStubServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cannedResponse))
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.API.MeteoblueEnabled = true
	cfg.API.MeteoblueEndpoint = srv.URL // 指向桩 server，不触真实外网
	t.Setenv(EnvAPIKey, "test-key-from-env")

	c := New(cfg, WithLogger(func(string, ...any) {}),
		WithSleep(func(time.Duration) {}),
		WithHTTPClient(srv.Client()))
	// New 已按 env 取 key；再显式覆盖端点以防默认拼接。
	c.Endpoint = srv.URL

	loc := shanghai(t)
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, loc)
	end := time.Date(2026, 8, 14, 0, 0, 0, 0, loc)
	targetNights := map[string]bool{"2026-08-12": true}

	rows, err := c.FetchSite(context.Background(), testSite(), start, end, targetNights)
	if err != nil {
		t.Fatalf("FetchSite 端到端失败：%v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("期望 1 行夜间记录，得到 %d 行", len(rows))
	}
	r := rows[0]
	if r.TimeISO != "2026-08-12T22:00" {
		t.Errorf("TimeISO 期望 2026-08-12T22:00，得到 %q", r.TimeISO)
	}
	if r.Rating != model.RATING_OK {
		t.Errorf("干净夜应判 %q，得到 %q（note=%s）", model.RATING_OK, r.Rating, r.Note)
	}
	if r.CloudLowSource.V != "meteoblue" {
		t.Errorf("CloudLowSource 应标 meteoblue，得到 %q", r.CloudLowSource.V)
	}
	if !r.Relation.Valid || r.Relation.V != relationMeteoblueNoGeometry {
		t.Errorf("Relation 应为 %q，得到 %v", relationMeteoblueNoGeometry, r.Relation)
	}
}

func TestFetchSiteHTTP4xxDoesNotRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := config.Default()
	cfg.API.MeteoblueEnabled = true
	t.Setenv(EnvAPIKey, "bad-key")

	c := New(cfg,
		WithSleep(func(time.Duration) {}),
		WithHTTPClient(srv.Client()))
	c.Endpoint = srv.URL

	_, err := c.FetchSite(context.Background(), testSite(),
		time.Now(), time.Now().Add(48*time.Hour), map[string]bool{"2026-08-12": true})
	if err == nil {
		t.Fatal("401 应返回错误")
	}
	if calls != 1 {
		t.Errorf("4xx 不应重试，期望 1 次调用，实际 %d 次", calls)
	}
}
