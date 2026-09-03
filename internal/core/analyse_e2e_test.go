package core

import (
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// seaBelowLevelValues 复制自 profile 包 TestEvaluateHourSeaBelow 夹具，
// 能反演出「机位下方有云层」→ REL_SEA_BELOW（云海在脚下）。
func seaBelowLevelValues() map[int]model.RawLevel {
	return map[int]model.RawLevel{
		1000: {CC: model.Num(10), GH: model.Num(110), RH: model.Num(70)},
		975:  {CC: model.Num(95), GH: model.Num(330), RH: model.Num(98)},
		950:  {CC: model.Num(98), GH: model.Num(560), RH: model.Num(99)},
		925:  {CC: model.Num(10), GH: model.Num(800), RH: model.Num(60)},
		900:  {CC: model.Num(0), GH: model.Num(1050), RH: model.Num(40)},
		850:  {CC: model.Num(0), GH: model.Num(1560), RH: model.Num(30)},
		800:  {CC: model.Num(0), GH: model.Num(2100), RH: model.Num(25)},
		700:  {CC: model.Num(0), GH: model.Num(3200), RH: model.Num(20)},
	}
}

// buildSeaBelowResponse 构造一条「前晚有雨 + 当前云海在脚下」的合成响应。
// 索引 0 = 前晚（含降水 prevPrecip），索引 1 = 当前拍摄时刻（wind/blh/precip 可调）。
func buildSeaBelowResponse(t *testing.T, curWind float64, curBLH model.OptFloat, prevPrecip, curPrecip float64) *api.Response {
	t.Helper()
	lv := seaBelowLevelValues()
	series := map[string][]model.OptFloat{}
	for _, p := range api.PressureLevels {
		cc, gh, rh := api.LevelVarNames(p)
		raw := lv[p]
		series[cc] = []model.OptFloat{raw.CC, raw.CC}
		series[gh] = []model.OptFloat{raw.GH, raw.GH}
		series[rh] = []model.OptFloat{raw.RH, raw.RH}
	}
	surface := func(v0, v1 float64) []model.OptFloat { return []model.OptFloat{model.Num(v0), model.Num(v1)} }
	series["temperature_2m"] = surface(12, 12)
	series["dew_point_2m"] = surface(4, 4)
	series["relative_humidity_2m"] = surface(58, 58)
	series["cloud_cover_low"] = surface(98, 98)
	series["cloud_cover_mid"] = surface(0, 0)
	series["cloud_cover_high"] = surface(0, 0)
	series["wind_speed_10m"] = []model.OptFloat{model.Num(2.0), model.Num(curWind)}
	series["weather_code"] = surface(0, 0)
	series["visibility"] = surface(30000, 30000)
	series["boundary_layer_height"] = []model.OptFloat{model.Num(450), curBLH}
	series["precipitation"] = []model.OptFloat{model.Num(prevPrecip), model.Num(curPrecip)}

	return &api.Response{
		Latitude:         30.026,
		Longitude:        119.007,
		Elevation:        1489.9,
		UTCOffsetSeconds: 28800,
		Timezone:         "Asia/Shanghai",
		Times: []time.Time{
			time.Date(2026, 8, 11, 23, 0, 0, 0, time.UTC), // 前晚（NightID 2026-08-11）
			time.Date(2026, 8, 12, 23, 0, 0, 0, time.UTC), // 当前拍摄时刻（NightID 2026-08-12）
		},
		Series: series,
	}
}

func TestAnalyseSiteCloudSeaCauseAppearsInNote(t *testing.T) {
	cfg := config.Default()
	resp := buildSeaBelowResponse(t, 2.0, model.Num(450), 3.0, 0.0)

	site := model.Site{Name: "测试山", Lat: 30.026, Lon: 119.007, Alt: 1489.9}
	rows := AnalyseSite(site, resp, nil, cfg)

	var cur *model.HourRow
	for i := range rows {
		if rows[i].Night == "2026-08-12" {
			cur = &rows[i]
		}
	}
	if cur == nil {
		t.Fatal("没找到当前夜(2026-08-12)的评估行")
	}
	if cur.CloudSea != "有" {
		t.Fatalf("当前夜云海几何应为「有」，实际 %q（说明 %q）", cur.CloudSea, cur.Note)
	}
	if cur.CloudSeaForm != "脚下型" {
		t.Fatalf("脚下型云海应标 CloudSeaForm=脚下型，实际 %q", cur.CloudSeaForm)
	}
	if !strings.Contains(cur.Note, "云海成因") {
		t.Fatalf("「主要诱因」列未出现云海成因加权，说明 %q", cur.Note)
	}
	if !strings.Contains(cur.Note, "高置信") {
		t.Fatalf("前晚有雨+静风+逆温+头顶通透应判高置信，说明 %q", cur.Note)
	}
	t.Logf("当前夜云海成因说明：%s", cur.Note)
}

func TestAnalyseSiteCloudSeaCauseLowConfidence(t *testing.T) {
	cfg := config.Default()
	// 风大(>3级)、无逆温(边界层缺测)、前晚无雨：仅几何成立 → 低置信。
	resp := buildSeaBelowResponse(t, 6.0, model.Missing(), 0.0, 0.0)

	site := model.Site{Name: "测试山", Lat: 30.026, Lon: 119.007, Alt: 1489.9}
	rows := AnalyseSite(site, resp, nil, cfg)

	var cur *model.HourRow
	for i := range rows {
		if rows[i].Night == "2026-08-12" {
			cur = &rows[i]
		}
	}
	if cur == nil {
		t.Fatal("没找到当前夜(2026-08-12)的评估行")
	}
	if cur.CloudSea != "有" {
		t.Fatalf("当前夜云海几何应为「有」，实际 %q", cur.CloudSea)
	}
	if !strings.Contains(cur.Note, "云海成因") {
		t.Fatalf("「主要诱因」列未出现云海成因加权，说明 %q", cur.Note)
	}
	if !strings.Contains(cur.Note, "低置信") {
		t.Fatalf("风大+无逆温+无前晚降水应判低置信，说明 %q", cur.Note)
	}
	t.Logf("当前夜云海成因说明：%s", cur.Note)
}
