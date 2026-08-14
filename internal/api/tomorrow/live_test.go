package tomorrow

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
)

const liveBody = `{"timelines":{"hourly":[
{"time":"2026-08-06T14:00:00Z","values":{"altimeterSetting":1016.42,"cloudBase":7.29,"cloudCeiling":7.9,"cloudCover":92.19,"dewPoint":-2.5,"evapotranspiration":0.447,"freezingRainIntensity":0,"humidity":15,"iceAccumulation":0,"precipitationProbability":0,"pressureSeaLevel":998.64,"pressureSurfaceLevel":724.24,"rainAccumulation":0,"rainIntensity":0,"sleetAccumulation":0,"sleetAccumulationLwe":0,"sleetIntensity":0,"snowAccumulation":0,"snowDepth":null,"snowIntensity":0,"temperature":26.17,"temperatureApparent":26.2,"uvHealthConcern":0,"uvIndex":0,"visibility":16,"weatherCode":1001,"windDirection":50,"windGust":8.9,"windSpeed":5.6}},
{"time":"2026-08-06T15:00:00Z","values":{"altimeterSetting":1016.62,"cloudBase":7.18,"cloudCeiling":7.3,"cloudCover":95.31,"dewPoint":-2.8,"evapotranspiration":0.379,"freezingRainIntensity":0,"humidity":16,"iceAccumulation":0,"precipitationProbability":0,"pressureSeaLevel":999.22,"pressureSurfaceLevel":724.4,"rainAccumulation":0,"rainIntensity":0,"sleetAccumulation":0,"sleetAccumulationLwe":0,"sleetIntensity":0,"snowAccumulation":0,"snowDepth":null,"snowIntensity":0,"temperature":25.29,"temperatureApparent":25.3,"uvHealthConcern":0,"uvIndex":0,"visibility":16,"weatherCode":1001,"windDirection":58,"windGust":7.4,"windSpeed":4.6}},
{"time":"2026-08-06T16:00:00Z","values":{"altimeterSetting":1016.72,"cloudBase":7,"cloudCeiling":7.2,"cloudCover":57.03,"dewPoint":-4,"evapotranspiration":0.321,"freezingRainIntensity":0,"humidity":15,"iceAccumulation":0,"precipitationProbability":0,"pressureSeaLevel":999.62,"pressureSurfaceLevel":724.47,"rainAccumulation":0,"rainIntensity":0,"sleetAccumulation":0,"sleetAccumulationLwe":0,"sleetIntensity":0,"snowAccumulation":0,"snowDepth":null,"snowIntensity":0,"temperature":24.3,"temperatureApparent":24.3,"uvHealthConcern":0,"uvIndex":0,"visibility":16,"weatherCode":1101,"windDirection":52,"windGust":6,"windSpeed":3.7}}
]}}`

func TestParseLiveResponseNoFieldSilentlyMissing(t *testing.T) {
	samples, err := parseForecast([]byte(liveBody))
	if err != nil {
		t.Fatalf("解析真实响应失败：%v", err)
	}
	if len(samples) != 3 {
		t.Fatalf("应解出 3 个时次，实际 %d", len(samples))
	}
	s := samples[0]

	fields := map[string]model.OptFloat{
		"CloudBaseRaw":         s.CloudBaseRaw,
		"CloudCeilingRaw":      s.CloudCeilingRaw,
		"CloudCover":           s.CloudCover,
		"VisibilityKm":         s.VisibilityKm,
		"HumidityPct":          s.HumidityPct,
		"WindSpeedMS":          s.WindSpeedMS,
		"WindGustMS":           s.WindGustMS,
		"TemperatureC":         s.TemperatureC,
		"DewPointC":            s.DewPointC,
		"PrecipProbabilityPct": s.PrecipProbabilityPct,
		"PressureSurfaceHPa":   s.PressureSurfaceHPa,
		"PressureSeaHPa":       s.PressureSeaHPa,
	}
	for name, v := range fields {
		if !v.Valid {
			t.Errorf("%s 在真实响应下缺测——多半是 json tag 拼写错误", name)
		}
	}
	if !s.WeatherCode.Valid {
		t.Error("WeatherCode 在真实响应下缺测——多半是 json tag 拼写错误")
	}

	mustOpt(t, s.CloudBaseRaw, 7.29, "CloudBaseRaw")
	mustOpt(t, s.CloudCeilingRaw, 7.9, "CloudCeilingRaw")
	mustOpt(t, s.WindSpeedMS, 5.6, "WindSpeedMS")
	mustOpt(t, s.WindGustMS, 8.9, "WindGustMS")
	mustOpt(t, s.DewPointC, -2.5, "DewPointC")
	mustOpt(t, s.PressureSurfaceHPa, 724.24, "PressureSurfaceHPa")
	mustOpt(t, s.PressureSeaHPa, 998.64, "PressureSeaHPa")

	if s.PressureSurfaceHPa.V >= s.PressureSeaHPa.V {
		t.Errorf("本站气压 %.2f 不该 ≥ 海平面气压 %.2f——两个气压串位了",
			s.PressureSurfaceHPa.V, s.PressureSeaHPa.V)
	}
	assertPlausibleTerrain(t, s, "冷湖实测抓包")

	const realAltM = 2790.0
	h := dualtrack.HModel(s.PressureSurfaceHPa.V, s.PressureSeaHPa.V)
	if gap := realAltM - h; gap < 0 || gap > 400 {
		t.Errorf("冷湖模式地形 %.1f m 与真实海拔 %.0f m 相差 %.1f m，"+
			"超出该点位实测的百米级范围", h, realAltM, gap)
	}
	if s.WeatherCode.V != 1001 {
		t.Errorf("WeatherCode = %d，期望 1001", s.WeatherCode.V)
	}

	if !s.PrecipProbabilityPct.Valid || s.PrecipProbabilityPct.V != 0 {
		t.Errorf("线上的 0 应解成有效值 0，实际 %+v", s.PrecipProbabilityPct)
	}
}

func TestParseLiveIntegerValuedFloat(t *testing.T) {
	samples, err := parseForecast([]byte(liveBody))
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	mustOpt(t, samples[2].CloudBaseRaw, 7, "整数形态的 cloudBase")
	mustOpt(t, samples[2].DewPointC, -4, "整数形态的 dewPoint")
}

func TestLiveMagnitudeAgreesWithPinnedKm(t *testing.T) {
	samples, err := parseForecast([]byte(liveBody))
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	raw := rawCloudBaseValues(samples)
	if err := CheckUnitSanity(UnitKilometer, raw); err != nil {
		t.Errorf("实测量级与配置的 km 应自洽，实际报警：%v", err)
	}

	agl, msl := convertHeight(samples[0].CloudBaseRaw, UnitKilometer, coldLakeAlt, DatumAGL)
	mustOpt(t, agl, 7290, "实测云底 AGL")
	mustOpt(t, msl, 7290+coldLakeAlt, "实测云底 MSL")
}

func TestProbeCountsAgainstQuota(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, liveBody)
	}))
	t.Cleanup(srv.Close)

	cfg := config.APIConfig{TomorrowEndpoint: srv.URL, CacheDir: dir}
	c := New(cfg, false, WithAPIKey("k", KeySourceEnv), WithHTTPClient(srv.Client()))
	if c.Quota == nil {
		t.Fatal("配置了 CacheDir，Client 应带上配额台账")
	}

	site := model.Site{Name: "冷湖", Lat: 38.75, Lon: 93.33, Alt: coldLakeAlt}
	if _, err := Probe(context.Background(), c, site, io.Discard); err != nil {
		t.Fatalf("探针失败：%v", err)
	}

	u, err := c.Quota.Snapshot()
	if err != nil {
		t.Fatalf("读取台账失败：%v", err)
	}
	if u.UsedHour != 1 || u.UsedDay != 1 {
		t.Errorf("探针应记 1 笔配额，实际 hour=%d day=%d", u.UsedHour, u.UsedDay)
	}
}

func TestProbeCountsQuotaEvenOn429(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"message":"rate limit"}`)
	}))
	t.Cleanup(srv.Close)

	cfg := config.APIConfig{TomorrowEndpoint: srv.URL, CacheDir: dir}
	c := New(cfg, false, WithAPIKey("k", KeySourceEnv), WithHTTPClient(srv.Client()))

	site := model.Site{Name: "冷湖", Lat: 38.75, Lon: 93.33, Alt: coldLakeAlt}
	if _, err := Probe(context.Background(), c, site, io.Discard); err == nil {
		t.Fatal("429 应返回错误")
	}

	u, err := c.Quota.Snapshot()
	if err != nil {
		t.Fatalf("读取台账失败：%v", err)
	}
	if u.UsedHour != 1 {
		t.Errorf("429 也应计入配额，实际 hour=%d", u.UsedHour)
	}
}

func TestProbeWorksWithoutLedger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, liveBody)
	}))
	t.Cleanup(srv.Close)

	cfg := config.APIConfig{TomorrowEndpoint: srv.URL}
	c := New(cfg, false, WithAPIKey("k", KeySourceEnv), WithHTTPClient(srv.Client()))
	if c.Quota != nil {
		t.Fatal("无 CacheDir 时不该有台账")
	}

	site := model.Site{Name: "冷湖", Lat: 38.75, Lon: 93.33, Alt: coldLakeAlt}
	if _, err := Probe(context.Background(), c, site, io.Discard); err != nil {
		t.Fatalf("无台账时探针应照常工作，实际：%v", err)
	}
}
