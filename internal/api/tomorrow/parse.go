package tomorrow

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/prophetcro/astro-mountain/internal/model"
)

const timeLayout = time.RFC3339

type forecastResponse struct {
	Timelines struct {
		Hourly []hourlyEntry `json:"hourly"`
	} `json:"timelines"`
}

type hourlyEntry struct {
	Time   string       `json:"time"`
	Values hourlyValues `json:"values"`
}

type hourlyValues struct {
	CloudBase    *float64 `json:"cloudBase"`
	CloudCeiling *float64 `json:"cloudCeiling"`
	CloudCover   *float64 `json:"cloudCover"`

	Visibility *float64 `json:"visibility"`
	Humidity   *float64 `json:"humidity"`

	WindSpeed *float64 `json:"windSpeed"`
	WindGust  *float64 `json:"windGust"`

	Temperature *float64 `json:"temperature"`
	DewPoint    *float64 `json:"dewPoint"`

	PrecipitationProbability *float64 `json:"precipitationProbability"`
	PressureSurfaceLevel     *float64 `json:"pressureSurfaceLevel"`
	PressureSeaLevel         *float64 `json:"pressureSeaLevel"`

	WeatherCode *int `json:"weatherCode"`
}

type Sample struct {
	TimeUTC time.Time

	TimeRaw string

	CloudBaseRaw model.OptFloat

	CloudCeilingRaw model.OptFloat

	CloudCover model.OptFloat

	VisibilityKm model.OptFloat

	HumidityPct model.OptFloat

	WindSpeedMS model.OptFloat

	WindGustMS model.OptFloat

	TemperatureC model.OptFloat

	DewPointC model.OptFloat

	PrecipProbabilityPct model.OptFloat

	PressureSurfaceHPa model.OptFloat

	PressureSeaHPa model.OptFloat

	WeatherCode OptCode
}

func (s Sample) DewSpreadC() model.OptFloat {
	return model.Sub(s.TemperatureC, s.DewPointC)
}

type OptCode struct {
	Valid bool
	V     int
}

func Code(v int) OptCode { return OptCode{Valid: true, V: v} }

func MissingCode() OptCode { return OptCode{} }

func (c OptCode) Or(fallback int) int {
	if c.Valid {
		return c.V
	}
	return fallback
}

func optCodeFromPtr(p *int) OptCode {
	if p == nil {
		return MissingCode()
	}
	return Code(*p)
}

func optFromPtr(p *float64) model.OptFloat {
	if p == nil {
		return model.Missing()
	}
	return model.NumOrMissing(*p)
}

func parseForecast(body []byte) ([]Sample, error) {
	var resp forecastResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 Tomorrow.io 响应失败：%w", err)
	}
	entries := resp.Timelines.Hourly
	if len(entries) == 0 {
		return nil, fmt.Errorf("Tomorrow.io 响应里没有 timelines.hourly 数据")
	}

	out := make([]Sample, 0, len(entries))
	badTime := 0
	for _, e := range entries {
		t, err := time.Parse(timeLayout, e.Time)
		if err != nil {
			badTime++
			continue
		}
		v := e.Values
		out = append(out, Sample{
			TimeUTC: t.UTC(),
			TimeRaw: e.Time,

			CloudBaseRaw:    optFromPtr(v.CloudBase),
			CloudCeilingRaw: optFromPtr(v.CloudCeiling),
			CloudCover:      optFromPtr(v.CloudCover),

			VisibilityKm:         optFromPtr(v.Visibility),
			HumidityPct:          optFromPtr(v.Humidity),
			WindSpeedMS:          optFromPtr(v.WindSpeed),
			WindGustMS:           optFromPtr(v.WindGust),
			TemperatureC:         optFromPtr(v.Temperature),
			DewPointC:            optFromPtr(v.DewPoint),
			PrecipProbabilityPct: optFromPtr(v.PrecipitationProbability),
			PressureSurfaceHPa:   optFromPtr(v.PressureSurfaceLevel),
			PressureSeaHPa:       optFromPtr(v.PressureSeaLevel),
			WeatherCode:          optCodeFromPtr(v.WeatherCode),
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("Tomorrow.io 响应里 %d 个时次的时间格式均无法解析（期望 RFC3339）",
			badTime)
	}
	return out, nil
}

func rawCloudBaseValues(samples []Sample) []float64 {
	out := make([]float64, 0, len(samples))
	for _, s := range samples {
		if s.CloudBaseRaw.Valid {
			out = append(out, s.CloudBaseRaw.V)
		}
	}
	return out
}

func MissingRate(samples []Sample) float64 {
	if len(samples) == 0 {
		return 1
	}
	missing := 0
	for _, s := range samples {
		if !s.CloudBaseRaw.Valid {
			missing++
		}
	}
	return float64(missing) / float64(len(samples))
}
