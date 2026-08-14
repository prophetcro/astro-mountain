package core

import (
	"context"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/report"
)

type stubMeteoblueFetcher struct{}

func (stubMeteoblueFetcher) Name() string { return "stub-meteoblue" }

func (stubMeteoblueFetcher) FetchSite(_ context.Context, site Site, _ time.Time, _ time.Time,
	_ map[string]bool) ([]model.HourRow, error) {
	return []model.HourRow{{
		Site:           site.Name,
		Lat:            site.Lat,
		Lon:            site.Lon,
		Alt:            site.Alt,
		TimeISO:        "2026-08-12T22:00",
		Time:           time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC),
		Night:          "2026-08-12",
		HasData:        true,
		Rating:         model.RATING_OK,
		CloudLowSource: model.Str("meteoblue"),
	}}, nil
}

// TestMeteoblueSourceDispatchProducesRows 验证：用户点名 --source meteoblue 时，
// 取数走 C 轨（MeteoblueFetcher）而非 Open-Meteo，产出的 HourRow 直接并入主 rows，
// 且 Meta.Source 正确署名为 Meteoblue，但不被误判为 Tomorrow 渲染路径。
func TestMeteoblueSourceDispatchProducesRows(t *testing.T) {
	t.Setenv("METEOBLUE_API_KEY", "dummy-key-never-sent")

	cfg := config.Default()
	cfg.API.MeteoblueEnabled = true

	e := NewEngine(cfg)
	e.MeteoblueFetcher = stubMeteoblueFetcher{}
	e.Now = func() time.Time { return time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC) }
	e.Logf = func(string, ...any) {}

	res := e.Run(context.Background(), RunParams{
		Peak:    "2026-08-12",
		Days:    0,
		Source:  SourceMeteoblue,
		Sites:   redlineOneSite(),
		NoCache: true,
		Quiet:   true,
		OutDir:  t.TempDir(),
	})

	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d，期望 0。errors=%v", res.ExitCode, res.Errors)
	}
	if len(res.Rows) == 0 {
		t.Fatal("Meteoblue 源应产出 rows，却为空——取数链路没被消费")
	}
	if res.Meta.Source != report.MetaSourceMeteoblue {
		t.Errorf("Meta.Source = %q，期望 %q。报告署名必须是本轮真正跑的那条轨。",
			res.Meta.Source, report.MetaSourceMeteoblue)
	}
	if report.IsTomorrowSource(res.Meta.Source) {
		t.Error("Meteoblue 不应被判为 Tomorrow 源，否则会误入 B 轨独立渲染分支")
	}
	if res.Rows[0].CloudLowSource.V != "meteoblue" {
		t.Errorf("CloudLowSource 应标 meteoblue（数据来源差异），得到 %q",
			res.Rows[0].CloudLowSource.V)
	}
}

// TestMeteoblueWiredNilSafe 验证 nil 接收者保护与 Deliverable 同值于 Wired。
func TestMeteoblueWiredNilSafe(t *testing.T) {
	var nilEngine *Engine
	if nilEngine.MeteoblueWired() {
		t.Error("nil Engine 不可能接线，MeteoblueWired() 却返回 true")
	}

	if (&Engine{}).MeteoblueWired() {
		t.Error("MeteoblueFetcher 为 nil 时 MeteoblueWired() 必须为 false")
	}
	if !(&Engine{MeteoblueFetcher: stubMeteoblueFetcher{}}).MeteoblueWired() {
		t.Error("已注入 MeteoblueFetcher，MeteoblueWired() 却返回 false")
	}
	if got, want := (&Engine{MeteoblueFetcher: stubMeteoblueFetcher{}}).MeteoblueDeliverable(),
		(&Engine{MeteoblueFetcher: stubMeteoblueFetcher{}}).MeteoblueWired(); got != want {
		t.Errorf("MeteoblueDeliverable() = %v，但 MeteoblueWired() = %v，二者必须同值", got, want)
	}
}
