// Package model 定义星空气象预报的核心数据模型。
//
// 这些类型被 api（数据抓取）、core（评级内核）、report（报告渲染）各层共享：
//   - 评级与云/天象关系标签（RATING_*、REL_*）
//   - 站点 Site 与逐小时评估行 HourRow
//   - 报告元信息 ReportMeta
//
// 字段上的 json tag 决定序列化结构，下游报告与前端直接消费，改动需同步。
package model

import "time"

// 评级结论：汇总表『结论』列的唯一权威判据。
// 严重程度由低到高：OK < WARN < BAD < NODATA。
const (
	// RATING_OK 通透，适合出片。
	RATING_OK = "✅通透"

	// RATING_WARN 有风险，需谨慎。
	RATING_WARN = "⚠️风险"

	// RATING_BAD 不宜拍摄。
	RATING_BAD = "🔴不宜"

	// RATING_NODATA 无数据，无法判定。
	RATING_NODATA = "❓无数据"
)

// RATING_CLEAR 是 RATING_OK 的别名，供旧调用方兼容使用。
const RATING_CLEAR = RATING_OK

// 双模型交叉对比的共识类别。
// 单小时分别由两个模式（ICON / GFS）评级后，据此归并成一句话结论。
const (
	// ConsensusBothOK 两个模式都判为通透。
	ConsensusBothOK = "both_ok"
	// ConsensusBothBad 两个模式都判为不宜/风险（至少一个不宜）。
	ConsensusBothBad = "both_bad"
	// ConsensusIconOnly 仅 ICON 判通透，GFS 否——分歧，ICON 偏乐观时需警惕。
	ConsensusIconOnly = "icon_only"
	// ConsensusGfsOnly 仅 GFS 判通透，ICON 否——分歧。
	ConsensusGfsOnly = "gfs_only"
	// ConsensusNoData 至少一个模式缺测，无法对比。
	ConsensusNoData = "nodata"
)

// ModelCompareRow 是双模型交叉对比的逐小时配对结果。
//
// 它独立于 HourRow，不进入默认轨 CSV/JSON 序列化（避免破坏红线字节一致性），
// 仅用于报告对比章节与终端摘要。每个 (站点, 整点) 由 ICON 与 GFS 两套评级配对得到。
type ModelCompareRow struct {
	Site       string `json:"site"`
	Night      string `json:"night"`
	TimeISO    string `json:"time"`
	TimeShort  string `json:"time_short"`
	Hour       int    `json:"hour"`
	IconRating string `json:"icon_rating"`
	GfsRating  string `json:"gfs_rating"`
	Consensus  string `json:"consensus"`
}

// ClassifyConsensus 由两个模式的逐小时评级判定共识类别。
// 通透仅认 RATING_OK；WARN 视为「未达通透」，与主结论口径一致。
func ClassifyConsensus(icon, gfs string) string {
	if icon == RATING_NODATA || gfs == RATING_NODATA {
		return ConsensusNoData
	}
	iconOK := icon == RATING_OK
	gfsOK := gfs == RATING_OK
	switch {
	case iconOK && gfsOK:
		return ConsensusBothOK
	case !iconOK && !gfsOK:
		return ConsensusBothBad
	case iconOK:
		return ConsensusIconOnly
	default:
		return ConsensusGfsOnly
	}
}

// WMO 天气现象代码常量（详见 WMO Weather interpretation codes）。
// 用于判断某小时是否含降水、雷暴，驱动 HasPrecip / 评级。
const (
	WMOCodeDrizzleLight  = 51
	WMOCodeDrizzleMod    = 53
	WMOCodeDrizzleDense  = 55
	WMOCodeFreezingDrizL = 56
	WMOCodeFreezingDrizD = 57
	WMOCodeRainSlight    = 61
	WMOCodeRainMod       = 63
	WMOCodeRainHeavy     = 65
	WMOCodeFreezingRainL = 66
	WMOCodeFreezingRainD = 67
	WMOCodeSnowSlight    = 71
	WMOCodeSnowMod       = 73
	WMOCodeSnowHeavy     = 75
	WMOCodeSnowGrains    = 77
	WMOCodeShowerSlight  = 80
	WMOCodeShowerMod     = 81
	WMOCodeShowerViolent = 82
	WMOCodeSnowShowerS   = 85
	WMOCodeSnowShowerD   = 86
	WMOCodeThunderstorm  = 95
	WMOCodeThunderstormH = 96
	WMOCodeThunderstormV = 99
)

// IsPrecipCode 判断 WMO 代码是否属于降水类（雨、冻雨、雪、阵雨、雷暴）。
func IsPrecipCode(code int) bool {
	switch code {
	case WMOCodeDrizzleLight, WMOCodeDrizzleMod, WMOCodeDrizzleDense,
		WMOCodeFreezingDrizL, WMOCodeFreezingDrizD,
		WMOCodeRainSlight, WMOCodeRainMod, WMOCodeRainHeavy,
		WMOCodeFreezingRainL, WMOCodeFreezingRainD,
		WMOCodeSnowSlight, WMOCodeSnowMod, WMOCodeSnowHeavy, WMOCodeSnowGrains,
		WMOCodeShowerSlight, WMOCodeShowerMod, WMOCodeShowerViolent,
		WMOCodeSnowShowerS, WMOCodeSnowShowerD,
		WMOCodeThunderstorm, WMOCodeThunderstormH, WMOCodeThunderstormV:
		return true
	}
	return false
}

// IsThunderstormCode 判断 WMO 代码是否属于雷暴类。
func IsThunderstormCode(code int) bool {
	switch code {
	case WMOCodeThunderstorm, WMOCodeThunderstormH, WMOCodeThunderstormV:
		return true
	}
	return false
}

// ratingOrder 给四种评级赋序，数值越大越严重，供 Worse 比较。
var ratingOrder = map[string]int{
	RATING_OK:     0,
	RATING_WARN:   1,
	RATING_BAD:    2,
	RATING_NODATA: 3,
}

// Worse 返回 a、b 中更严重（序更大）的评级；平手时取 a。
func Worse(a, b string) string {
	if ratingOrder[a] >= ratingOrder[b] {
		return a
	}
	return b
}

// 云/天象关系标签：描述机位与云层的几何相对位置，只看几何、不判点位好坏。
const (
	REL_CLEAR     = "CLEAR"
	REL_SEA_BELOW = "SEA_BELOW"
	REL_IN_CLOUD  = "IN_CLOUD"
	REL_OVERHEAD  = "OVERHEAD"
	REL_NODATA    = "NODATA"
)

// REL_BASE_BELOW_UNKNOWN 云底低于机位，脚下云海与机位在云中无法区分。
const REL_BASE_BELOW_UNKNOWN = "BASE_BELOW_UNKNOWN"

// RelLabels 把关系标签映射为中文展示文案。
var RelLabels = map[string]string{
	REL_CLEAR:              "全层无云",
	REL_SEA_BELOW:          "云海在脚下",
	REL_IN_CLOUD:           "机位在云中",
	REL_OVERHEAD:           "云在头顶",
	REL_NODATA:             "无数据",
	REL_BASE_BELOW_UNKNOWN: "云底低于机位（脚下云海/机位在云中不可判）",
}

// Site 表示一个观测站点及其静态元数据。
type Site struct {
	Name     string  `json:"name"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Alt      float64 `json:"alt"`
	Region   string  `json:"region,omitempty"`
	Timezone string  `json:"timezone,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
	Note     string  `json:"note,omitempty"`
}

// IsEnabled 站点是否启用：Enabled 为 nil（未显式设置）时视为启用。
func (s Site) IsEnabled() bool {
	return s.Enabled == nil || *s.Enabled
}

// Surface 地表层逐小时气象要素（对应 Open-Meteo surface 变量）。
type Surface struct {
	Temperature2m       OptFloat
	DewPoint2m          OptFloat
	RelativeHumidity2m  OptFloat
	CloudCoverLow       OptFloat
	CloudCoverMid       OptFloat
	CloudCoverHigh      OptFloat
	WindSpeed10m        OptFloat
	Visibility          OptFloat
	BoundaryLayerHeight OptFloat
	FreezingLevelHeight OptFloat

	Precipitation OptFloat

	WeatherCode OptFloat
}

// HasPrecip 该小时是否存在降水：直接降水量 > 0，或天气代码属于降水类。
func (s Surface) HasPrecip() bool {
	if s.Precipitation.Valid && s.Precipitation.V > 0 {
		return true
	}
	if s.WeatherCode.Valid && IsPrecipCode(int(s.WeatherCode.V)) {
		return true
	}
	return false
}

// RawLevel 气压层原始观测：云量 CC、位势高 GH、相对湿度 RH。
type RawLevel struct {
	CC OptFloat
	GH OptFloat
	RH OptFloat
}

// LayerInfo 单层云的几何与湿度信息（MSL 为海拔，AGL 为离地高度）。
type LayerInfo struct {
	BaseMSL   OptFloat `json:"base_msl"`
	TopMSL    OptFloat `json:"top_msl"`
	Thickness OptFloat `json:"thickness"`
	MaxCC     OptFloat `json:"max_cc"`
	MaxRH     OptFloat `json:"max_rh"`
	OpenTop   bool     `json:"open_top"`
	OpenBase  bool     `json:"open_base"`
	RHOnly    bool     `json:"rh_only"`
}

// HourRow 单个站点单小时的完整评估行，是报告的数据核心单元。
//
// 三个易混字段的语义边界：
//   - Relation：云层几何（云海在脚下 / 头顶通透 / 机位在云中 / 头顶薄云），只看几何、不判点位好坏；
//   - Rating：✅/⚠️/🔴/❓ 唯一权威判据，由 core 评级得出；
//   - Note：把结论压低的根因（浓雾/降水/在云中/薄云兜底等），由 MainReason 承载。
type HourRow struct {
	Site      string    `json:"site"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Alt       float64   `json:"alt"`
	TimeISO   string    `json:"time"`
	TimeShort string    `json:"time_short"`
	Hour      int       `json:"hour"`
	Night     string    `json:"night"`
	HasData   bool      `json:"has_data"`
	Time      time.Time `json:"-"`

	LevelsTotal int `json:"levels_total"`
	LevelsAbove int `json:"levels_above"`

	CloudBaseMSL   OptFloat `json:"cloud_base_msl"`
	CloudBaseAGL   OptFloat `json:"cloud_base_agl"`
	CloudTopMSL    OptFloat `json:"cloud_top_msl"`
	CloudTopAGL    OptFloat `json:"cloud_top_agl"`
	CloudThickness OptFloat `json:"cloud_thickness"`
	LayerMaxCC     OptFloat `json:"layer_max_cc"`

	Relation NullString `json:"relation"`
	Rating   string     `json:"rating"`
	Note     string     `json:"note"`

	// CloudSea 有无云海提示（独立于雾/降水的可见性判断）：
	// 有=机位下方存在连续云面（云海在脚下几何成立）；无=无。取值 "有"/"无"。
	CloudSea string `json:"cloud_sea"`

	CloudLow       OptFloat   `json:"cloud_low"`
	CloudLowSource NullString `json:"cloud_low_source"`
	CloudMid       OptFloat   `json:"cloud_mid"`
	CloudHigh      OptFloat   `json:"cloud_high"`

	Visibility OptFloat `json:"visibility"`
	Temp       OptFloat `json:"temp"`
	Dew        OptFloat `json:"dew"`
	Spread     OptFloat `json:"spread"`
	WindMS     OptFloat `json:"wind_ms"`

	BoundaryLayerHeight OptFloat `json:"boundary_layer_height"`
	FreezingLevelHeight OptFloat `json:"freezing_level_height"`
	LCLAGLEst           OptFloat `json:"lcl_agl_est"`

	SunAlt    OptFloat `json:"sun_alt"`
	MoonAlt   OptFloat `json:"moon_alt"`
	MoonIllum OptFloat `json:"moon_illum"`
	MoonPhase string   `json:"moon_phase"`
	GCAlt     OptFloat `json:"gc_alt"`
	AstroDark bool     `json:"astro_dark"`

	Layers []LayerInfo `json:"layers"`
}

// ReportMeta 报告元信息：模型、时间窗、站点列表与统计摘要。
type ReportMeta struct {
	Models              string     `json:"models"`
	Start               string     `json:"start"`
	End                 string     `json:"end"`
	Nights              []string   `json:"nights"`
	NightsDesc          string     `json:"nights_desc"`
	Peak                NullString `json:"peak"`
	Days                *int       `json:"days"`
	Timezone            string     `json:"timezone"`
	UTCOffsetHours      float64    `json:"utc_offset_hours"`
	Sites               []Site     `json:"sites"`
	HoursTotal          int        `json:"hours_total"`
	HoursWithData       int        `json:"hours_with_data"`
	VisibilityAvailable bool       `json:"visibility_available"`
	GeneratedAt         string     `json:"generated_at"`
	Source              string     `json:"source"`
	Disclaimer          string     `json:"disclaimer"`
}
