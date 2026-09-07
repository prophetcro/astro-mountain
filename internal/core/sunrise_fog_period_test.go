package core

import (
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

// buildFogResp 构造一个以 sunrise 为中心、每小时一点的 api.Response 骨架，
// 仅填充近地雾所需的地面要素（能见度缺测 → 走 RH 代理判据）。
// 每个时次的雾档由调用方按需 setFogAt 设置；未设置的时次 RH 无效 → 判为「无」。
func buildFogResp(sunrise time.Time, n int) *api.Response {
	resp := &api.Response{
		Times:  make([]time.Time, n),
		Series: map[string][]model.OptFloat{},
	}
	for _, name := range []string{
		"relative_humidity_2m", "temperature_2m", "dew_point_2m", "wind_speed_10m", "visibility",
	} {
		resp.Series[name] = make([]model.OptFloat, n)
	}
	for i := 0; i < n; i++ {
		resp.Times[i] = sunrise.Add(time.Duration(i-n/2) * time.Hour)
	}
	return resp
}

// setFogAt 设置单个时次的近地要素。wind 固定 4m/s（不触发辐射雾风速修正），
// 能见度缺测（走 RH 代理），确保档位仅由 RH+温露差决定、可精确预期。
func setFogAt(resp *api.Response, idx int, rh, temp, dew, wind float64) {
	resp.Series["relative_humidity_2m"][idx] = model.Num(rh)
	resp.Series["temperature_2m"][idx] = model.Num(temp)
	resp.Series["dew_point_2m"][idx] = model.Num(dew)
	resp.Series["wind_speed_10m"][idx] = model.Num(wind)
	resp.Series["visibility"][idx] = model.Missing()
}

func TestAssessDawnGroundFogPeriods_SingleRun(t *testing.T) {
	cfg := config.Default()
	// sunrise 落在 idx6（05:00）。n=13 → Times[idx]=sunrise+(idx-6)h。
	// 窗口 [sunrise-8h, sunrise+2h] = idx[-2..8] → idx0..8 在窗内，idx9..12 窗外。
	sunrise := time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)
	const n = 13
	resp := buildFogResp(sunrise, n)

	// idx0-1 无；idx2-4 强；idx5-6 中（含 idx6=sunrise）；idx7-8 无（仍在窗内）。
	setFogAt(resp, 0, 50, 20, 10, 4)
	setFogAt(resp, 1, 50, 20, 10, 4)
	setFogAt(resp, 2, 97, 15.5, 14, 4) // 强
	setFogAt(resp, 3, 97, 15.5, 14, 4) // 强
	setFogAt(resp, 4, 97, 15.5, 14, 4) // 强
	setFogAt(resp, 5, 92, 15, 12, 4)   // 中
	setFogAt(resp, 6, 92, 15, 12, 4)   // 中（=sunrise 05:00）
	setFogAt(resp, 7, 50, 20, 10, 4)   // 无
	setFogAt(resp, 8, 50, 20, 10, 4)   // 无

	periods := assessDawnGroundFogPeriods(resp, sunrise, cfg)
	if len(periods) != 1 {
		t.Fatalf("期望 1 个辐射雾时段，实际 %d: %+v", len(periods), periods)
	}
	p := periods[0]
	if p.PeakLevel != profile.FOG_STRONG {
		t.Errorf("峰值档位应为 强，实际 %q", p.PeakLevel)
	}
	// 起点 = idx2 = sunrise-4h = 01:00；终点 = 最后雾时次 idx6(05:00)+1h = 06:00。
	if want := sunrise.Add(-4 * time.Hour); !p.Start.Equal(want) {
		t.Errorf("时段起点应为 %s，实际 %s", want.Format("15:04"), p.Start.Format("15:04"))
	}
	if want := sunrise.Add(1 * time.Hour); !p.End.Equal(want) {
		t.Errorf("时段终点应为 %s，实际 %s", want.Format("15:04"), p.End.Format("15:04"))
	}
	// 峰值时刻应是 run 内最强档首次出现：idx2（05:00 之前的 01:00），而非 sunrise 本身。
	if want := sunrise.Add(-4 * time.Hour); !p.PeakHour.Equal(want) {
		t.Errorf("峰值时刻应为 %s，实际 %s", want.Format("15:04"), p.PeakHour.Format("15:04"))
	}
}

func TestAssessDawnGroundFogPeriods_TwoRuns(t *testing.T) {
	cfg := config.Default()
	sunrise := time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)
	const n = 13
	resp := buildFogResp(sunrise, n)

	// idx2-3 强；idx4 无（断开）；idx5-6 强 → 应得 2 个独立时段。
	setFogAt(resp, 2, 97, 15.5, 14, 4)
	setFogAt(resp, 3, 97, 15.5, 14, 4)
	setFogAt(resp, 4, 50, 20, 10, 4) // 断开
	setFogAt(resp, 5, 97, 15.5, 14, 4)
	setFogAt(resp, 6, 97, 15.5, 14, 4)

	periods := assessDawnGroundFogPeriods(resp, sunrise, cfg)
	if len(periods) != 2 {
		t.Fatalf("期望 2 个辐射雾时段，实际 %d: %+v", len(periods), periods)
	}
	// 前段 idx2-3（01:00–02:00）→ 终点 03:00；后段 idx5-6（04:00–05:00）→ 起点 04:00。
	// idx4（03:00）为「无」断开，两段应不相连、中间留 1h 间隙。
	if want := sunrise.Add(-2 * time.Hour); !periods[0].End.Equal(want) { // 03:00
		t.Errorf("前段终点应为 %s，实际 %s", want.Format("15:04"), periods[0].End.Format("15:04"))
	}
	if want := sunrise.Add(-1 * time.Hour); !periods[1].Start.Equal(want) { // 04:00
		t.Errorf("后段起点应为 %s，实际 %s", want.Format("15:04"), periods[1].Start.Format("15:04"))
	}
}

func TestAssessDawnGroundFogPeriods_NoFogNil(t *testing.T) {
	cfg := config.Default()
	sunrise := time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)
	resp := buildFogResp(sunrise, 13)
	// 全部未设置 → 所有时次 RH 无效 → 无成片雾。
	if got := assessDawnGroundFogPeriods(resp, sunrise, cfg); got != nil {
		t.Errorf("全窗口无雾时应为 nil，实际 %+v", got)
	}
	if got := assessDawnGroundFogPeriods(nil, sunrise, cfg); got != nil {
		t.Errorf("空响应应为 nil，实际 %+v", got)
	}
}

// TestAssessDawnGroundFogPeriods_OnlyWeakSkipped 锁死：仅「弱」雾（轻雾）不构成时段，
// 时段门槛是「中」及以上（地面雾可拍），避免把边缘轻雾渲染成一长串时段。
func TestAssessDawnGroundFogPeriods_OnlyWeakSkipped(t *testing.T) {
	cfg := config.Default()
	sunrise := time.Date(2026, 9, 16, 5, 0, 0, 0, time.UTC)
	resp := buildFogResp(sunrise, 13)
	// 一段连续的「弱」：RH 92 但温露差 5℃（>spreadModerate 4）→ 弱。
	for _, idx := range []int{2, 3, 4, 5, 6} {
		setFogAt(resp, idx, 92, 15, 10, 4) // spread=5 → 弱
	}
	if got := assessDawnGroundFogPeriods(resp, sunrise, cfg); got != nil {
		t.Errorf("仅弱雾不应形成时段，实际 %+v", got)
	}
}
