package tomorrow

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func mustOpt(t *testing.T, got model.OptFloat, want float64, field string) {
	t.Helper()
	if !got.Valid {
		t.Errorf("%s 应为有效值 %g，实际缺测", field, want)
		return
	}
	if math.Abs(got.V-want) > 1e-9 {
		t.Errorf("%s = %g，期望 %g", field, got.V, want)
	}
}

func mustMissing(t *testing.T, got model.OptFloat, field string) {
	t.Helper()
	if got.Valid {
		t.Errorf("%s 应为缺测，实际拿到有效值 %g", field, got.V)
	}
}

func assertPlausibleTerrain(t *testing.T, s Sample, label string) {
	t.Helper()
	if !s.PressureSurfaceHPa.Valid || !s.PressureSeaHPa.Valid {
		return
	}
	h := dualtrack.HModel(s.PressureSurfaceHPa.V, s.PressureSeaHPa.V)
	if math.IsNaN(h) {
		t.Errorf("%s：气压对 %.2f/%.2f 解不出地形高度",
			label, s.PressureSurfaceHPa.V, s.PressureSeaHPa.V)
		return
	}
	const loM, hiM = -500.0, 9000.0
	if h < loM || h > hiM {
		t.Errorf("%s：气压对 %.2f/%.2f 解出地形高度 %.1f m，超出 [%.0f,%.0f]。"+
			"多半是把不同点位/不同时次的气压凑成了一对",
			label, s.PressureSurfaceHPa.V, s.PressureSeaHPa.V, h, loM, hiM)
	}
}

const coldLakeBody = `{
  "timelines": {
    "hourly": [
      {
        "time": "2026-08-06T13:00:00Z",
        "values": {
          "cloudBase": 7.98,
          "cloudCeiling": 8.7,
          "cloudCover": 96.88,
          "visibility": 16,
          "humidity": 11,
          "windSpeed": 2.9,
          "windGust": 5,
          "temperature": 27.94,
          "dewPoint": -5.1,
          "precipitationProbability": 0,
          "pressureSurfaceLevel": 723.47,
          "pressureSeaLevel": 996.06,
          "weatherCode": 1001,
          "evapotranspiration": 0.132,
          "sleetAccumulation": 0
        }
      }
    ]
  }
}`

func TestParseColdLakeSampleAllFields(t *testing.T) {
	samples, err := parseForecast([]byte(coldLakeBody))
	if err != nil {
		t.Fatalf("解析实测样本失败：%v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("应解出 1 个时次，实际 %d", len(samples))
	}
	s := samples[0]

	wantTime := time.Date(2026, 8, 6, 13, 0, 0, 0, time.UTC)
	if !s.TimeUTC.Equal(wantTime) {
		t.Errorf("TimeUTC = %v，期望 %v", s.TimeUTC, wantTime)
	}
	if s.TimeRaw != "2026-08-06T13:00:00Z" {
		t.Errorf("TimeRaw = %q，原始串应原样保留", s.TimeRaw)
	}

	mustOpt(t, s.CloudBaseRaw, 7.98, "CloudBaseRaw")
	mustOpt(t, s.CloudCeilingRaw, 8.7, "CloudCeilingRaw")
	mustOpt(t, s.CloudCover, 96.88, "CloudCover")
	mustOpt(t, s.VisibilityKm, 16, "VisibilityKm")
	mustOpt(t, s.HumidityPct, 11, "HumidityPct")
	mustOpt(t, s.WindSpeedMS, 2.9, "WindSpeedMS")
	mustOpt(t, s.WindGustMS, 5, "WindGustMS")
	mustOpt(t, s.TemperatureC, 27.94, "TemperatureC")
	mustOpt(t, s.DewPointC, -5.1, "DewPointC")
	mustOpt(t, s.PrecipProbabilityPct, 0, "PrecipProbabilityPct")
	mustOpt(t, s.PressureSurfaceHPa, 723.47, "PressureSurfaceHPa")
	mustOpt(t, s.PressureSeaHPa, 996.06, "PressureSeaHPa")

	assertPlausibleTerrain(t, s, "冷湖 fixture")

	if !s.WeatherCode.Valid || s.WeatherCode.V != 1001 {
		t.Errorf("WeatherCode = %+v，期望 {true 1001}", s.WeatherCode)
	}

	if s.CloudBaseRaw.V > 100 {
		t.Errorf("云底原始值不该在解析层被换算，实际 %g", s.CloudBaseRaw.V)
	}
}

func TestParseIgnoresUndeclaredFields(t *testing.T) {
	if _, err := parseForecast([]byte(coldLakeBody)); err != nil {
		t.Fatalf("未声明字段应被忽略，实际报错：%v", err)
	}
}

func TestNullVersusZeroForEveryField(t *testing.T) {

	cases := []struct {
		jsonKey string
		get     func(Sample) model.OptFloat
	}{
		{"cloudBase", func(s Sample) model.OptFloat { return s.CloudBaseRaw }},
		{"cloudCeiling", func(s Sample) model.OptFloat { return s.CloudCeilingRaw }},
		{"cloudCover", func(s Sample) model.OptFloat { return s.CloudCover }},
		{"visibility", func(s Sample) model.OptFloat { return s.VisibilityKm }},
		{"humidity", func(s Sample) model.OptFloat { return s.HumidityPct }},
		{"windSpeed", func(s Sample) model.OptFloat { return s.WindSpeedMS }},
		{"windGust", func(s Sample) model.OptFloat { return s.WindGustMS }},
		{"temperature", func(s Sample) model.OptFloat { return s.TemperatureC }},
		{"dewPoint", func(s Sample) model.OptFloat { return s.DewPointC }},
		{"precipitationProbability", func(s Sample) model.OptFloat { return s.PrecipProbabilityPct }},
		{"pressureSurfaceLevel", func(s Sample) model.OptFloat { return s.PressureSurfaceHPa }},
		{"pressureSeaLevel", func(s Sample) model.OptFloat { return s.PressureSeaHPa }},
	}

	body := func(key, raw string) []byte {
		v := map[string]any{}
		if raw != "" {
			var parsed any
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				t.Fatalf("用例自身的 JSON 片段有误：%v", err)
			}
			v[key] = parsed
		}
		doc := map[string]any{
			"timelines": map[string]any{
				"hourly": []any{
					map[string]any{"time": "2026-08-06T13:00:00Z", "values": v},
				},
			},
		}
		b, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("构造用例响应失败：%v", err)
		}
		return b
	}

	for _, c := range cases {
		t.Run(c.jsonKey, func(t *testing.T) {

			got, err := parseForecast(body(c.jsonKey, "null"))
			if err != nil {
				t.Fatalf("解析 null 失败：%v", err)
			}
			mustMissing(t, c.get(got[0]), c.jsonKey+"=null")

			got, err = parseForecast(body(c.jsonKey, ""))
			if err != nil {
				t.Fatalf("解析缺席字段失败：%v", err)
			}
			mustMissing(t, c.get(got[0]), c.jsonKey+" 缺席")

			got, err = parseForecast(body(c.jsonKey, "0"))
			if err != nil {
				t.Fatalf("解析 0 失败：%v", err)
			}
			mustOpt(t, c.get(got[0]), 0, c.jsonKey+"=0")

			got, err = parseForecast(body(c.jsonKey, "-12.5"))
			if err != nil {
				t.Fatalf("解析负值失败：%v", err)
			}
			mustOpt(t, c.get(got[0]), -12.5, c.jsonKey+"=-12.5")
		})
	}
}

func TestWeatherCodeNullVersusZero(t *testing.T) {
	parse := func(raw string) OptCode {
		t.Helper()
		body := `{"timelines":{"hourly":[{"time":"2026-08-06T13:00:00Z",` +
			`"values":{"weatherCode":` + raw + `}}]}}`
		s, err := parseForecast([]byte(body))
		if err != nil {
			t.Fatalf("解析 weatherCode=%s 失败：%v", raw, err)
		}
		return s[0].WeatherCode
	}

	if c := parse("null"); c.Valid {
		t.Errorf("weatherCode=null 应缺测，实际 %+v", c)
	}
	if c := parse("0"); !c.Valid || c.V != 0 {
		t.Errorf("weatherCode=0 应为有效的 0，实际 %+v", c)
	}
	if c := parse("1001"); !c.Valid || c.V != 1001 {
		t.Errorf("weatherCode=1001 解析错误，实际 %+v", c)
	}

	if got := MissingCode().Or(-1); got != -1 {
		t.Errorf("MissingCode().Or(-1) = %d，期望 -1", got)
	}
	if got := Code(0).Or(-1); got != 0 {
		t.Errorf("Code(0).Or(-1) = %d，期望 0——有效的 0 不该被兜底吃掉", got)
	}
}

func TestExtremeValuesArePreservedNotClamped(t *testing.T) {
	body := `{"timelines":{"hourly":[{"time":"2026-08-06T13:00:00Z",` +
		`"values":{"cloudBase":7980,"pressureSurfaceLevel":1e300,` +
		`"pressureSeaLevel":1e300,` +
		`"temperature":-273.15,"humidity":100}}]}}`
	s, err := parseForecast([]byte(body))
	if err != nil {
		t.Fatalf("解析极值失败：%v", err)
	}
	mustOpt(t, s[0].CloudBaseRaw, 7980, "极大云底")
	mustOpt(t, s[0].PressureSurfaceHPa, 1e300, "极大气压")
	mustOpt(t, s[0].PressureSeaHPa, 1e300, "极大海压")
	mustOpt(t, s[0].TemperatureC, -273.15, "绝对零度")
	mustOpt(t, s[0].HumidityPct, 100, "湿度上限")
}

func TestNonFiniteBecomesMissing(t *testing.T) {
	if got := optFromPtr(ptrF(math.NaN())); got.Valid {
		t.Errorf("NaN 应缺测，实际 %+v", got)
	}
	if got := optFromPtr(ptrF(math.Inf(1))); got.Valid {
		t.Errorf("+Inf 应缺测，实际 %+v", got)
	}
	if got := optFromPtr(ptrF(math.Inf(-1))); got.Valid {
		t.Errorf("-Inf 应缺测，实际 %+v", got)
	}
	if got := optFromPtr(nil); got.Valid {
		t.Errorf("nil 应缺测，实际 %+v", got)
	}
	mustOpt(t, optFromPtr(ptrF(0)), 0, "指针指向 0")
}

func ptrF(v float64) *float64 { return &v }

func TestBadTimeEntriesAreSkippedNotFatal(t *testing.T) {
	body := `{"timelines":{"hourly":[
		{"time":"不是时间","values":{"cloudBase":1}},
		{"time":"2026-08-06T13:00:00Z","values":{"cloudBase":2}},
		{"time":"","values":{"cloudBase":3}}
	]}}`
	s, err := parseForecast([]byte(body))
	if err != nil {
		t.Fatalf("应跳过坏时次而非报错，实际：%v", err)
	}
	if len(s) != 1 {
		t.Fatalf("应只剩 1 个好时次，实际 %d", len(s))
	}
	mustOpt(t, s[0].CloudBaseRaw, 2, "存活时次的云底")
}

func TestAllBadTimeEntriesIsError(t *testing.T) {
	body := `{"timelines":{"hourly":[{"time":"x","values":{}},{"time":"y","values":{}}]}}`
	if _, err := parseForecast([]byte(body)); err == nil {
		t.Fatal("全部时次时间无法解析时应报错")
	}
}

func TestEmptyOrMalformedBodyIsError(t *testing.T) {
	cases := map[string]string{
		"空 hourly":    `{"timelines":{"hourly":[]}}`,
		"无 timelines": `{}`,
		"非 JSON":      `not json`,
		"空 body":      ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseForecast([]byte(body)); err == nil {
				t.Error("应报错，实际解析成功")
			}
		})
	}
}

func TestDewSpreadMissingPropagates(t *testing.T) {
	full := Sample{TemperatureC: model.Num(27.94), DewPointC: model.Num(-5.1)}
	mustOpt(t, full.DewSpreadC(), 33.04, "露点差")

	noDew := Sample{TemperatureC: model.Num(27.94), DewPointC: model.Missing()}
	mustMissing(t, noDew.DewSpreadC(), "露点缺测时的露点差")

	noTemp := Sample{TemperatureC: model.Missing(), DewPointC: model.Num(-5.1)}
	mustMissing(t, noTemp.DewSpreadC(), "气温缺测时的露点差")

	saturated := Sample{TemperatureC: model.Num(3), DewPointC: model.Num(3)}
	mustOpt(t, saturated.DewSpreadC(), 0, "饱和时的露点差")
}

func TestMissingRateMatchesMeasuredColdLake(t *testing.T) {

	samples := make([]Sample, 121)
	for i := range samples {
		if i < 40 {
			samples[i].CloudBaseRaw = model.Missing()
		} else {
			samples[i].CloudBaseRaw = model.Num(7.98)
		}
	}
	got := MissingRate(samples)
	want := 40.0 / 121.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("缺测率 = %g，期望 %g", got, want)
	}
	if got < 0.30 || got > 0.36 {
		t.Errorf("实测冷湖缺测率应在 33%% 附近，实际 %.4f", got)
	}
}

func TestMissingRateEdgeCases(t *testing.T) {
	if got := MissingRate(nil); got != 1 {
		t.Errorf("空样本的缺测率应为 1（全缺），实际 %g", got)
	}
	allGood := []Sample{{CloudBaseRaw: model.Num(0)}, {CloudBaseRaw: model.Num(7.98)}}
	if got := MissingRate(allGood); got != 0 {
		t.Errorf("全有效（含真实的 0）缺测率应为 0，实际 %g", got)
	}
	allBad := []Sample{{CloudBaseRaw: model.Missing()}}
	if got := MissingRate(allBad); got != 1 {
		t.Errorf("全缺测的缺测率应为 1，实际 %g", got)
	}
}

func TestRawCloudBaseValuesSkipsMissing(t *testing.T) {
	samples := []Sample{
		{CloudBaseRaw: model.Num(7.98)},
		{CloudBaseRaw: model.Missing()},
		{CloudBaseRaw: model.Num(0.1)},
	}
	got := rawCloudBaseValues(samples)
	if len(got) != 2 {
		t.Fatalf("应收到 2 个有效值，实际 %d：%v", len(got), got)
	}
	if got[0] != 7.98 || got[1] != 0.1 {
		t.Errorf("有效值顺序或内容错误：%v", got)
	}
}
