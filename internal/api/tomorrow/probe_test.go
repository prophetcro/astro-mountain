package tomorrow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func newProbeClient(t *testing.T, handler http.HandlerFunc, key string) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := config.APIConfig{TomorrowEndpoint: srv.URL}
	return New(cfg, false, WithAPIKey(key, KeySourceEnv), WithHTTPClient(srv.Client()))
}

func TestProbePrintsValuesAndLength(t *testing.T) {
	c := newProbeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") == "" {
			t.Errorf("探针请求缺少 apikey")
		}
		if r.URL.Query().Get("timesteps") != "1h" {
			t.Errorf("探针请求缺少 timesteps=1h")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"timelines": map[string]any{
				"hourly": []map[string]any{
					{"time": "2024-01-01T00:00:00Z", "values": map[string]any{"cloudBase": 100.0}},
					{"time": "2024-01-01T01:00:00Z", "values": map[string]any{"cloudBase": 110.0}},
					{"time": "2024-01-01T02:00:00Z", "values": map[string]any{"cloudBase": 120.0}},
					{"time": "2024-01-01T03:00:00Z", "values": map[string]any{"cloudBase": 130.0}},
				},
			},
		})
	}, "TESTKEY")

	var sb strings.Builder
	site := model.Site{Name: "测试点", Lat: 28.2, Lon: 86.9, Alt: 5200}
	res, err := Probe(context.Background(), c, site, &sb)
	if err != nil {
		t.Fatalf("Probe 失败: %v", err)
	}
	out := sb.String()

	if res.HourlyCount != 4 {
		t.Fatalf("HourlyCount 应为 4，实际 %d", res.HourlyCount)
	}
	if !strings.Contains(out, "timelines.hourly 数组长度 : 4") {
		t.Fatalf("输出未包含 hourly 长度，got:\n%s", out)
	}
	if !strings.Contains(out, "cloudBase") || !strings.Contains(out, "100") {
		t.Fatalf("输出未包含前几个时次的 values，got:\n%s", out)
	}

	if strings.Contains(out, "TESTKEY") {
		t.Fatalf("输出泄漏了明文 API key，got:\n%s", out)
	}
}

func TestProbeNoKey(t *testing.T) {
	c := New(config.APIConfig{}, false)
	var sb strings.Builder
	site := model.Site{Name: "x", Lat: 1, Lon: 2, Alt: 0}
	_, err := Probe(context.Background(), c, site, &sb)
	if !errors.Is(err, ErrNoAPIKey) {
		t.Fatalf("无 key 时期望 ErrNoAPIKey，实际 %v", err)
	}
}

func TestProbeNon200(t *testing.T) {
	c := newProbeClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":401,"message":"unauthorized"}`))
	}, "BADKEY")

	var sb strings.Builder
	site := model.Site{Name: "x", Lat: 1, Lon: 2, Alt: 0}
	_, err := Probe(context.Background(), c, site, &sb)
	if err == nil {
		t.Fatal("期望非 200 返回错误")
	}
	out := sb.String()
	if !strings.Contains(out, "401") {
		t.Fatalf("期望输出包含 401，got:\n%s", out)
	}

	if strings.Contains(out, "BADKEY") {
		t.Fatalf("非 200 输出泄漏了明文 API key，got:\n%s", out)
	}
}
