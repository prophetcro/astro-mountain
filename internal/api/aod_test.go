package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func TestFetchAOD_ParsesAndAlignsWallClock(t *testing.T) {
	body := `{"hourly":{"time":["2026-09-06T03:00","2026-09-06T04:00","2026-09-06T05:00","2026-09-06T06:00"],"aerosol_optical_depth":[0.35,0.36,0.37,null]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("hourly"); got != "aerosol_optical_depth" {
			t.Errorf("hourly 参数 = %q，期望 aerosol_optical_depth", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := New(config.APIConfig{CacheEnabled: false}, false, WithAirQualityEndpoint(srv.URL))
	site := model.Site{Lat: 30.0, Lon: 118.0, Name: "测试点"}
	start := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)

	got, err := client.FetchAOD(context.Background(), site, start, end)
	if err != nil {
		t.Fatalf("FetchAOD 失败：%v", err)
	}

	// 日出 05:00 的墙钟键（UTC 承载），与 forecast 响应 Times 同口径。
	key := time.Date(2026, 9, 6, 5, 0, 0, 0, time.UTC)
	v, ok := got[key]
	if !ok {
		t.Fatalf("AOD 映射缺少 05:00 键；现有键：%v", mapKeys(got))
	}
	if !v.Valid || v.V != 0.37 {
		t.Fatalf("05:00 AOD = %+v，期望 Valid 0.37", v)
	}

	// 06:00 为 null → Invalid（缺测，下游按缺失处理而非 0）。
	nullKey := time.Date(2026, 9, 6, 6, 0, 0, 0, time.UTC)
	nv, ok := got[nullKey]
	if !ok || nv.Valid {
		t.Fatalf("06:00 AOD 应为缺测（Invalid），实际 %+v ok=%v", nv, ok)
	}
}

func TestFetchAOD_ErrorBodyReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":true,"reason":"Invalid location"}`))
	}))
	defer srv.Close()

	client := New(config.APIConfig{CacheEnabled: false}, false, WithAirQualityEndpoint(srv.URL))
	_, err := client.FetchAOD(context.Background(), model.Site{Name: "x"},
		time.Now(), time.Now().Add(24*time.Hour))
	if err == nil {
		t.Fatal("接口返回错误体却未报错（不可静默失败）")
	}
}

func mapKeys(m map[time.Time]model.OptFloat) []time.Time {
	out := make([]time.Time, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
