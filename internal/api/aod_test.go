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

// TestFetchAOD_RetriesOnHorizonExceeded 锁死「CAMS 时效超窗自动夹紧重试」：
// 日出报告把 end 多算一天（end_date=2026-09-14）超出 CAMS 最大可用日 2026-09-13，
// 首次请求被 400 拒绝；FetchAOD 应解析错误体里的最大日期、夹紧窗口重试，
// 拿到 2026-09-13 当天的 AOD 而非整站丢弃。这是 23 站点同时丢 AOD 的真实回归点。
func TestFetchAOD_RetriesOnHorizonExceeded(t *testing.T) {
	valid := `{"hourly":{"time":["2026-09-13T03:00","2026-09-13T04:00","2026-09-13T05:00","2026-09-13T06:00"],"aerosol_optical_depth":[0.30,0.31,0.32,null]}}`
	rangeErr := `{"error":true,"reason":"Parameter 'end_date' is out of allowed range from 2013-01-01 to 2026-09-13"}`

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		calls++
		if r.URL.Query().Get("end_date") == "2026-09-14" {
			// 第一次：超窗，返回服务端真实错误体（含允许的最大日期）。
			_, _ = w.Write([]byte(rangeErr))
			return
		}
		// 夹紧后重试：end_date=2026-09-13，返回合法数据。
		_, _ = w.Write([]byte(valid))
	}))
	defer srv.Close()

	client := New(config.APIConfig{CacheEnabled: false}, false, WithAirQualityEndpoint(srv.URL))
	site := model.Site{Lat: 30.0, Lon: 118.0, Name: "测试点"}
	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC) // 比 CAMS 最大可用日多一天

	got, err := client.FetchAOD(context.Background(), site, start, end)
	if err != nil {
		t.Fatalf("夹紧重试后仍报错：%v（calls=%d）", err, calls)
	}
	if calls < 2 {
		t.Fatalf("期望至少重试一次（calls=%d），实际未重试", calls)
	}

	// 日出 05:00 的墙钟键（UTC 承载）应有数据 0.32。
	key := time.Date(2026, 9, 13, 5, 0, 0, 0, time.UTC)
	v, ok := got[key]
	if !ok || !v.Valid || v.V != 0.32 {
		t.Fatalf("夹紧重试后 09-13 05:00 AOD = %+v ok=%v，期望 Valid 0.32", v, ok)
	}
}

// TestFetchAOD_RangeErrorBeyondHorizon 锁死「整体超出时效须如实报错、不静默」：
// 请求区间整体在 CAMS 最大可用日之后，夹紧后 start > max → 不重试、返回错误，
// 交由调用方按「AOD 缺失」诚实降级（而非伪造数据）。
func TestFetchAOD_RangeErrorBeyondHorizon(t *testing.T) {
	rangeErr := `{"error":true,"reason":"Parameter 'end_date' is out of allowed range from 2013-01-01 to 2026-09-10"}`
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(rangeErr))
	}))
	defer srv.Close()

	client := New(config.APIConfig{CacheEnabled: false}, false, WithAirQualityEndpoint(srv.URL))
	// 请求 09-20~09-21，远在最大可用日 09-10 之后。
	_, err := client.FetchAOD(context.Background(), model.Site{Name: "x"},
		time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("整体超出 CAMS 时效却未报错（不可静默失败）")
	}
	if calls != 1 {
		t.Fatalf("整体超时效不应重试（calls=%d）", calls)
	}
}

func mapKeys(m map[time.Time]model.OptFloat) []time.Time {
	out := make([]time.Time, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
