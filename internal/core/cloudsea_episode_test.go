package core

import (
	"math"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// mathAbs 是 math.Abs 的薄封装，让测试代码读起来更接近「差值比较」。
func mathAbs(x float64) float64 { return math.Abs(x) }

// makeCloudSeaResp 构造一个「连续 9 小时都有云海」的 api.Response：
// 时间轴覆盖当地 23:00 到次日 07:00（北京时间，对应 UTC 时间轴上的 9 个整点）。
//
// 云海形态：900hPa 全程有 60% 云量（位势高 983m），机位 1442m，
// 机位下方存在云海。地面低云量 ≥40% 以触发 CloudSea="有" 判定。
func makeCloudSeaResp(t *testing.T) *api.Response {
	t.Helper()

	resp := &api.Response{
		Latitude:         31.047, // 牛草山
		Longitude:        116.259,
		Elevation:        1442,
		UTCOffsetSeconds: 28800,
		Timezone:         "Asia/Shanghai",
	}

	// 当地时间起点 2026-09-15 23:00（即夜窗口首 hour），共 9 个整点。
	// 注意：api.Response.Times 里的时刻用 UTC 承载但已加过 UTCOffsetSeconds，
	// 所以直接写当地时刻即可，下游拿 .Hour() 就是当地钟点。
	start := time.Date(2026, 9, 15, 23, 0, 0, 0, time.UTC)
	for i := 0; i < 9; i++ {
		resp.Times = append(resp.Times, start.Add(time.Duration(i)*time.Hour))
	}

	// 地面变量：低云量 60%（≥40% 阈值），其它正常。
	sur := map[string][]float64{
		"temperature_2m":       {18, 17, 16, 15, 14, 13, 13, 14, 16},
		"dew_point_2m":         {14, 14, 14, 14, 14, 13, 13, 14, 15},
		"relative_humidity_2m": {85, 88, 92, 95, 98, 98, 96, 90, 85},
		"cloud_cover_low":      {60, 60, 60, 60, 60, 60, 60, 60, 60},
		"cloud_cover_mid":      {20, 20, 20, 20, 20, 20, 20, 20, 20},
		"cloud_cover_high":     {10, 10, 10, 10, 10, 10, 10, 10, 10},
		"wind_speed_10m":       {1, 1, 1, 1, 1, 1, 1, 1, 1},
		"visibility":           {20000, 20000, 20000, 20000, 20000, 20000, 20000, 20000, 20000},
		"weather_code":         {0, 0, 0, 0, 0, 0, 0, 0, 0},
	}
	// 气压层：900hPa 有云（脚下云海），850/800 无云。
	all := map[string][]float64{
		"cloud_cover_900hPa":         {60, 60, 60, 60, 60, 60, 60, 60, 60},
		"cloud_cover_850hPa":         {0, 0, 0, 0, 0, 0, 0, 0, 0},
		"cloud_cover_800hPa":         {0, 0, 0, 0, 0, 0, 0, 0, 0},
		"geopotential_height_900hPa": {983, 983, 983, 983, 983, 983, 983, 983, 983},
		"geopotential_height_850hPa": {1477, 1477, 1477, 1477, 1477, 1477, 1477, 1477, 1477},
		"geopotential_height_800hPa": {1996, 1996, 1996, 1996, 1996, 1996, 1996, 1996, 1996},
		"relative_humidity_900hPa":   {90, 90, 90, 90, 90, 90, 90, 90, 90},
		"relative_humidity_850hPa":   {50, 50, 50, 50, 50, 50, 50, 50, 50},
		"relative_humidity_800hPa":   {40, 40, 40, 40, 40, 40, 40, 40, 40},
	}
	for k, v := range sur {
		all[k] = v
	}

	resp.Series = make(map[string][]model.OptFloat, len(all))
	for name, vs := range all {
		optVals := make([]model.OptFloat, len(vs))
		for j, v := range vs {
			optVals[j] = model.Num(v)
		}
		resp.Series[name] = optVals
	}
	resp.HourlyVars = api.BuildHourlyVars(true)
	return resp
}

// TestCollectCloudSeaEpisodesForNight_Basic 连续 9 小时都有云海，但夜窗口 [22,6]
// 只含 23:00–06:00 共 8 个 hour，应合并为 1 个 episode。
func TestCollectCloudSeaEpisodesForNight_Basic(t *testing.T) {
	cfg := config.Default()
	resp := makeCloudSeaResp(t)
	site := Site{Name: "牛草山", Lat: 31.047, Lon: 116.259, Alt: 1442}

	episodes := CollectCloudSeaEpisodesForNight(site, resp, "2026-09-15", cfg)

	if len(episodes) != 1 {
		t.Fatalf("连续 8 个夜窗小时云海应合并为 1 个 episode，实际 %d 个", len(episodes))
	}
	ep := episodes[0]

	// NightIDOf(07:00) = 07:00 - 12h = 19:00 = "2026-09-15"，
	// 但 InNightWindow(7) 在默认 [22,6] 下为 false，所以 07:00 被排除。
	wantStart := time.Date(2026, 9, 15, 23, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	if !ep.Start.Equal(wantStart) {
		t.Errorf("Start = %s，期望 %s", ep.Start.Format(time.RFC3339), wantStart.Format(time.RFC3339))
	}
	if !ep.End.Equal(wantEnd) {
		t.Errorf("End = %s，期望 %s", ep.End.Format(time.RFC3339), wantEnd.Format(time.RFC3339))
	}

	// 云顶距机位高差：DetectLayers 在 900hPa 有云、850hPa 无云的形态下，
	// 把云顶向上插值到 (40-60)/(0-60)≈0.333 比例（阈值=40%），得到 983+0.333*(1477-983)≈1147m。
	// 故 TopAGL = 1442 - 1147 ≈ 295m（这是云海顶距机位真实形态，非输入的 900hPa 高度）。
	wantTopAGL := 1442.0 - 1147.4
	if mathAbs(ep.TopAGL-wantTopAGL) > 2.0 {
		t.Errorf("TopAGL = %.1f，期望 ≈ %.1f", ep.TopAGL, wantTopAGL)
	}
	if ep.Submerged {
		t.Error("云顶 1147m < 机位 1442m，不应被淹没")
	}
	if ep.PeakThickness <= 0 {
		t.Errorf("PeakThickness 应 > 0，实际 %.1f", ep.PeakThickness)
	}
	if ep.HoursCount != 8 {
		t.Errorf("HoursCount = %d，期望 8（夜窗 [22,6] 内的 hour 数）", ep.HoursCount)
	}
}

// TestCollectCloudSeaEpisodesForNight_NoSea 把 900hPa 云量清零 → 无云海 → 空结果。
func TestCollectCloudSeaEpisodesForNight_NoSea(t *testing.T) {
	cfg := config.Default()
	resp := makeCloudSeaResp(t)
	nine := []float64{0, 0, 0, 0, 0, 0, 0, 0, 0}
	resp.Series["cloud_cover_900hPa"] = make([]model.OptFloat, len(nine))
	for i, v := range nine {
		resp.Series["cloud_cover_900hPa"][i] = model.Num(v)
	}

	site := Site{Name: "牛草山", Lat: 31.047, Lon: 116.259, Alt: 1442}
	episodes := CollectCloudSeaEpisodesForNight(site, resp, "2026-09-15", cfg)
	if len(episodes) != 0 {
		t.Errorf("整夜无云应返回空，实际 %d 个 episode", len(episodes))
	}
}

// TestCollectCloudSeaEpisodesForNight_WrongNight 指定不存在的日期 → 空结果。
func TestCollectCloudSeaEpisodesForNight_WrongNight(t *testing.T) {
	cfg := config.Default()
	resp := makeCloudSeaResp(t)
	site := Site{Name: "牛草山", Lat: 31.047, Lon: 116.259, Alt: 1442}
	episodes := CollectCloudSeaEpisodesForNight(site, resp, "2099-01-01", cfg)
	if len(episodes) != 0 {
		t.Errorf("日期不匹配应返回空，实际 %d 个 episode", len(episodes))
	}
}
