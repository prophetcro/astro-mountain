package core_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api/meteoblue"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// cannedMeteoblueResponse 与 api/meteoblue 包内桩响应同构（字段名严格对照
// openapi.yml 的 basic-3h + clouds-3h 合并包——免费档默认组合），含一个夜间时刻
// （RFC3339 +08:00，data_3h 块）。
const cannedMeteoblueResponse = `{
  "data_3h": {
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

func e2eSite() []model.Site {
	return []model.Site{{Name: "星辰山", Lat: 28.2656, Lon: 119.3788, Alt: 1000.0, Timezone: "Asia/Shanghai"}}
}

// TestMeteoblueEndToEndViaLocalStub 把「真实」meteoblue 客户端（端点指向本地桩
// server）注入 Engine，走完整链路：取数 → ParseResponse → EvaluateResponse →
// 并入主 rows → 渲染 Markdown，验证报告署名与免责声明正确指向 C 轨。
//
// 放在 core_test（外部测试包）是为了避免 core 内部测试包直接 import api/meteoblue
// 形成的导入环（api/meteoblue 为接口断言需要 import core）。
func TestMeteoblueEndToEndViaLocalStub(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过端到端桩 server 测试")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cannedMeteoblueResponse))
	}))
	defer srv.Close()

	t.Setenv(meteoblue.EnvAPIKey, "test-key-from-env")

	cfg := config.Default()
	cfg.API.MeteoblueEnabled = true
	cfg.API.MeteoblueEndpoint = srv.URL

	client := meteoblue.New(cfg,
		meteoblue.WithLogger(func(string, ...any) {}),
		meteoblue.WithSleep(func(time.Duration) {}),
		meteoblue.WithHTTPClient(srv.Client()),
	)
	client.Endpoint = srv.URL

	e := core.NewEngine(cfg)
	e.MeteoblueFetcher = client
	e.Now = func() time.Time { return time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC) }
	e.Logf = func(string, ...any) {}

	res := e.Run(context.Background(), core.RunParams{
		Peak:      "2026-08-12",
		Days:      0,
		Source:    core.SourceMeteoblue,
		Sites:     e2eSite(),
		NoCache:   true,
		ExportCSV: true,
		Quiet:     true,
		OutDir:    t.TempDir(),
	})

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d，期望 0。errors=%v", res.ExitCode, res.Errors)
	}
	if len(res.Rows) == 0 {
		t.Fatal("端到端未产出任何行")
	}
	if res.Meta.Source != "Meteoblue" {
		t.Errorf("Meta.Source = %q，期望 Meteoblue", res.Meta.Source)
	}

	md, err := os.ReadFile(res.ReportPath)
	if err != nil {
		t.Fatalf("读取 Markdown 报告失败：%v", err)
	}
	text := string(md)
	if !strings.Contains(text, "Meteoblue") {
		t.Errorf("Markdown 报告未署名 Meteoblue：\n%s", text)
	}
	if !strings.Contains(text, "不反演云海几何") {
		t.Errorf("Markdown 报告未声明 C 轨能力边界（不反演云海几何）：\n%s", text)
	}
	// 诚实性红线：C 轨报告绝不可出现 A 轨署名，否则用户会误以为能判云海。
	if strings.Contains(text, "Open-Meteo 免费 API") {
		t.Errorf("C 轨报告混入了 A 轨署名「Open-Meteo 免费 API」，诚实性红线被破：\n%s", text)
	}
}
