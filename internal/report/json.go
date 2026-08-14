package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

type fieldLabelsJSON struct{}

func (fieldLabelsJSON) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, f := range CSVFields {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(f)
		if err != nil {
			return nil, err
		}
		label, ok := FieldLabels[f]
		if !ok {
			label = f
		}
		val, err := json.Marshal(label)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

type configExport struct {
	CloudCoverThreshold   float64 `json:"cloud_cover_threshold"`
	RHThresholdLow        float64 `json:"rh_threshold_low"`
	RHThresholdHigh       float64 `json:"rh_threshold_high"`
	RHLowLayerPressureMin int     `json:"rh_low_layer_pressure_min"`

	FogVisibilityM  float64 `json:"fog_visibility_m"`
	HazeVisibilityM float64 `json:"haze_visibility_m"`
	FogCalmWindMS   float64 `json:"fog_calm_wind_ms"`
	FogProxyRHHigh  float64 `json:"fog_proxy_rh_high"`
	FogProxyRHWarn  float64 `json:"fog_proxy_rh_warn"`

	OverheadSevereCC        float64 `json:"overhead_severe_cc"`
	LayerMinHalfSpanFrac    float64 `json:"layer_min_half_span_frac"`
	MinLevelHeightMSL       float64 `json:"min_level_height_msl"`
	CloudSeaMaxDepthM       float64 `json:"cloud_sea_max_depth_m"`
	ProfileLowcloudCrossChk float64 `json:"profile_lowcloud_crosscheck"`
	CloudSeaSuspectLowcloud float64 `json:"cloud_sea_suspect_lowcloud"`

	DewSpreadC   float64 `json:"dew_spread_c"`
	LCLWarnAGLM  float64 `json:"lcl_warn_agl_m"`
	LCLAlertAGLM float64 `json:"lcl_alert_agl_m"`

	NightStartHour int `json:"night_start_hour"`
	NightEndHour   int `json:"night_end_hour"`
	CoreStartHour  int `json:"core_start_hour"`
	CoreEndHour    int `json:"core_end_hour"`

	AstroDarkSunAlt float64 `json:"astro_dark_sun_alt"`
	MoonBrightIllum float64 `json:"moon_bright_illum"`

	Retries       int     `json:"retries"`
	BackoffFactor float64 `json:"backoff_factor"`
}

func newConfigExport(cfg config.Config) configExport {
	t, w, a := cfg.Thresh, cfg.Window, cfg.API
	return configExport{
		CloudCoverThreshold:   t.CloudCoverThreshold,
		RHThresholdLow:        t.RHThresholdLow,
		RHThresholdHigh:       t.RHThresholdHigh,
		RHLowLayerPressureMin: t.RHLowLayerPressureMin,

		FogVisibilityM:  t.FogVisibilityM,
		HazeVisibilityM: t.HazeVisibilityM,
		FogCalmWindMS:   t.FogCalmWindMS,
		FogProxyRHHigh:  t.FogProxyRHHigh,
		FogProxyRHWarn:  t.FogProxyRHWarn,

		OverheadSevereCC:        t.OverheadSevereCC,
		LayerMinHalfSpanFrac:    t.LayerMinHalfSpanFrac,
		MinLevelHeightMSL:       t.MinLevelHeightMSL,
		CloudSeaMaxDepthM:       t.CloudSeaMaxDepthM,
		ProfileLowcloudCrossChk: t.ProfileLowcloudCrossChk,
		CloudSeaSuspectLowcloud: t.CloudSeaSuspectLowcloud,

		DewSpreadC:   t.DewSpreadC,
		LCLWarnAGLM:  t.LCLWarnAGLM,
		LCLAlertAGLM: t.LCLAlertAGLM,

		NightStartHour: w.NightStartHour,
		NightEndHour:   w.NightEndHour,
		CoreStartHour:  w.CoreStartHour,
		CoreEndHour:    w.CoreEndHour,

		AstroDarkSunAlt: t.AstroDarkSunAlt,
		MoonBrightIllum: t.MoonBrightIllum,

		Retries:       a.Retries,
		BackoffFactor: a.BackoffFactor,
	}
}

type jsonPayload struct {
	FieldLabels fieldLabelsJSON  `json:"field_labels"`
	Meta        model.ReportMeta `json:"meta"`
	Config      configExport     `json:"config"`
	Rows        []model.HourRow  `json:"rows"`
}

func BuildJSON(meta model.ReportMeta, rows []model.HourRow, cfg config.Config) ([]byte, error) {
	sorted := append([]model.HourRow(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].TimeISO != sorted[j].TimeISO {
			return sorted[i].TimeISO < sorted[j].TimeISO
		}
		return sorted[i].Site < sorted[j].Site
	})
	if sorted == nil {
		sorted = []model.HourRow{}
	}

	payload := jsonPayload{
		Meta:   meta,
		Config: newConfigExport(cfg),
		Rows:   sorted,
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return nil, fmt.Errorf("序列化 JSON 失败：%w", err)
	}
	return buf.Bytes(), nil
}

func ExportJSON(path string, meta model.ReportMeta, rows []model.HourRow,
	cfg config.Config) error {

	data, err := BuildJSON(meta, rows, cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入 JSON 文件 %s 失败：%w", path, err)
	}
	return nil
}
