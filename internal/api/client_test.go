package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func testSite() model.Site {
	return model.Site{Name: "星辰山", Lat: 28.2656, Lon: 119.3788, Alt: 1000}
}

func newTestClient(endpoint string, opts ...Option) *Client {
	cfg := config.APIConfig{
		Endpoint:      endpoint,
		Models:        "icon_seamless",
		Timezone:      "Asia/Shanghai",
		Retries:       2,
		BackoffFactor: 0,
	}
	base := []Option{
		WithCache(Disabled()),
		WithSleep(func(time.Duration) {}),
	}
	return New(cfg, false, append(base, opts...)...)
}

func TestBuildURL_FlatBuffersFormat(t *testing.T) {
	c := newTestClient("https://api.open-meteo.com/v1/forecast")
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)

	raw := c.BuildURL(testSite(), start, end, "", BuildHourlyVars(true))
	if !strings.Contains(raw, "format=flatbuffers") {
		t.Fatalf("URL 必须带 format=flatbuffers，实际：%s", raw)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("URL 无法解析：%v", err)
	}
	q := u.Query()
	want := map[string]string{
		"format":          "flatbuffers",
		"models":          "icon_seamless",
		"timezone":        "Asia/Shanghai",
		"wind_speed_unit": "ms",
		"start_date":      "2026-08-12",
		"end_date":        "2026-08-13",
		"latitude":        "28.2656",
		"longitude":       "119.3788",
		"elevation":       "1000",
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("查询参数 %s = %q，期望 %q", k, got, v)
		}
	}
	if got, w := q.Get("hourly"), strings.Join(BuildHourlyVars(true), ","); got != w {
		t.Errorf("hourly 变量列表不一致\n got=%s\nwant=%s", got, w)
	}
}

func TestBuildURL_Deterministic(t *testing.T) {
	c := newTestClient("https://api.open-meteo.com/v1/forecast")
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC)

	a := c.BuildURL(testSite(), start, end, "", BuildHourlyVars(true))
	b := c.BuildURL(testSite(), start, end, "", BuildHourlyVars(true))
	if a != b {
		t.Errorf("同参数两次构造的 URL 不同：\n%s\n%s", a, b)
	}
	if KeyOf(a) != KeyOf(b) {
		t.Error("缓存键不稳定")
	}

	if KeyOf(a) == KeyOf(strings.Replace(a, "&format=flatbuffers", "", 1)) {
		t.Error("带/不带 format 参数的缓存键相同，旧 JSON 缓存有被误读的风险")
	}
}

func TestFetchSite_DropsOptionalVarsOnError(t *testing.T) {
	const utcOffset int32 = 28800
	localMidnight := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).Unix()
	payload := buildStream(t, midnightEpoch(localMidnight, utcOffset), 3600, utcOffset,
		"Asia/Shanghai", []fbVar{
			{variable: 47, altitude: 2, values: []float32{20.5, 21.5}},
		})

	var gotURLs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURLs = append(gotURLs, r.URL.String())
		if strings.Contains(r.URL.Query().Get("hourly"), "visibility") {

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"reason":"visibility is not supported","error":true}`))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	resp, vars, err := c.FetchSite(context.Background(), testSite(),
		time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC), "")
	if err != nil {
		t.Fatalf("降级重试后仍失败：%v", err)
	}
	if len(gotURLs) != 2 {
		t.Fatalf("期望请求 2 次（含可选 → 剔除后重试），实际 %d 次", len(gotURLs))
	}
	if !strings.Contains(gotURLs[0], "visibility") {
		t.Error("第一次请求应带上可选变量")
	}
	if strings.Contains(gotURLs[1], "visibility") {
		t.Error("第二次请求应已剔除可选变量")
	}
	for i, u := range gotURLs {
		if !strings.Contains(u, "format=flatbuffers") {
			t.Errorf("第 %d 次请求缺少 format=flatbuffers：%s", i+1, u)
		}
	}
	if got, want := len(vars), len(BuildHourlyVars(false)); got != want {
		t.Errorf("返回的变量数 = %d，期望 %d", got, want)
	}
	if v := resp.At("temperature_2m", 0); !v.Valid || v.V != 20.5 {
		t.Errorf("temperature_2m[0] = %v（valid=%v），期望 20.5", v.V, v.Valid)
	}
}

func TestFetchSite_RetriesOn5xx(t *testing.T) {
	const utcOffset int32 = 28800
	localMidnight := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).Unix()
	payload := buildStream(t, midnightEpoch(localMidnight, utcOffset), 3600, utcOffset,
		"Asia/Shanghai", []fbVar{
			{variable: 47, altitude: 2, values: []float32{18}},
		})

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("upstream boom"))
			return
		}
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	day := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	resp, _, err := c.FetchSite(context.Background(), testSite(), day, day, "")
	if err != nil {
		t.Fatalf("重试后仍失败：%v", err)
	}
	if calls != 2 {
		t.Errorf("期望重试 1 次（共 2 次调用），实际 %d 次", calls)
	}
	if v := resp.At("temperature_2m", 0); !v.Valid || v.V != 18 {
		t.Errorf("temperature_2m[0] = %v（valid=%v），期望 18", v.V, v.Valid)
	}
}

func TestDoOnce_AcceptsBinary(t *testing.T) {
	var accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"reason":"stop","error":true}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, _, _ = c.doOnce(context.Background(), srv.URL)

	if !strings.Contains(accept, "application/octet-stream") {
		t.Errorf("Accept 头应包含 application/octet-stream，实际 %q", accept)
	}
	if !strings.Contains(accept, "application/json") {
		t.Errorf("Accept 头应同时接受 JSON 错误体，实际 %q", accept)
	}
}

func TestSnippet(t *testing.T) {
	if got := snippet([]byte(`{"reason":"bad","error":true}`)); got != `{"reason":"bad","error":true}` {
		t.Errorf("JSON 错误体应原样保留，实际 %q", got)
	}

	binary := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0x7f, 0x00}
	got := snippet(binary)
	if !strings.HasPrefix(got, "<二进制") {
		t.Errorf("二进制响应应转成十六进制描述，实际 %q", got)
	}
	for _, r := range got {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			t.Fatalf("错误信息里仍残留控制字符：%q", got)
		}
	}

	long := strings.Repeat("中", 500)
	cut := snippet([]byte(long))
	if !strings.HasSuffix(cut, "…") {
		t.Errorf("超长文本应被截断，实际长度 %d", len([]rune(cut)))
	}
	if strings.Contains(cut, "\ufffd") {
		t.Error("截断产生了乱码字符")
	}
}
