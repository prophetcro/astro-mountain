package dualtrack

import (
	"math"
	"time"

	"github.com/prophetcro/astro-mountain/internal/model"
)

type TerrainFidelity string

const (
	TerrainUnknown TerrainFidelity = "UNKNOWN"

	TerrainFaithful TerrainFidelity = "FAITHFUL"

	TerrainCoarse TerrainFidelity = "COARSE"

	TerrainFlattened TerrainFidelity = "FLATTENED"
)

const (
	TerrainFaithfulMaxM = 150.0

	TerrainCoarseMaxM = 500.0
)

var TerrainFidelityLabels = map[TerrainFidelity]string{
	TerrainUnknown:   "保真度不可知",
	TerrainFaithful:  "模式地形贴合机位",
	TerrainCoarse:    "模式地形粗化",
	TerrainFlattened: "模式削平孤立峰",
}

func (f TerrainFidelity) Label() string {
	if s, ok := TerrainFidelityLabels[f]; ok {
		return s
	}
	return string(f)
}

func ClassifyTerrainFidelity(deltaHM float64) TerrainFidelity {
	if math.IsNaN(deltaHM) || math.IsInf(deltaHM, 0) {
		return TerrainUnknown
	}
	switch {
	case deltaHM >= -TerrainFaithfulMaxM:
		return TerrainFaithful
	case deltaHM > -TerrainCoarseMaxM:
		return TerrainCoarse
	default:
		return TerrainFlattened
	}
}

func ClassifyTerrainFidelityOpt(deltaH model.OptFloat) TerrainFidelity {
	if !deltaH.Valid {
		return TerrainUnknown
	}
	return ClassifyTerrainFidelity(deltaH.V)
}

type NoDataReason string

const (
	NoDataNone NoDataReason = ""

	KeyMissing NoDataReason = "KEY_MISSING"

	SemanticFailure NoDataReason = "SEMANTIC_FAILURE"

	RoundQuotaDown NoDataReason = "ROUND_QUOTA_DOWN"

	OutOfHorizon NoDataReason = "OUT_OF_HORIZON"

	AmbiguousBase NoDataReason = "AMBIGUOUS_BASE"
)

var NoDataReasonLabels = map[NoDataReason]string{
	NoDataNone:      "",
	KeyMissing:      "关键缺失",
	SemanticFailure: "语义失效",
	RoundQuotaDown:  "配额耗尽",
	OutOfHorizon:    "超预报窗",
	AmbiguousBase:   "云底低于机位不可判",
}

func (r NoDataReason) Label() string {
	if s, ok := NoDataReasonLabels[r]; ok {
		return s
	}
	return string(r)
}

type HourVerdict struct {
	TimeLocal time.Time

	TimeUTC time.Time

	HModelM model.OptFloat

	DeltaH model.OptFloat

	CloudBaseAGLM model.OptFloat

	CloudBaseAboveSite model.OptFloat

	Rel string

	Rating string

	NoDataReason NoDataReason

	TerrainFidelity TerrainFidelity

	SeaBelowUnknown bool

	Note string
}

func (v HourVerdict) IsNoData() bool { return v.Rating == model.RATING_NODATA }

func (v HourVerdict) IsAmbiguous() bool { return v.Rel == model.REL_BASE_BELOW_UNKNOWN }

type TrackCapabilities struct {
	HasCloudTopData bool

	SeaBelowUnknown bool
}

type TrackResult struct {
	SiteID string

	Active bool

	QuotaExhausted bool

	NextAvailable *time.Time

	Rows []HourVerdict

	Capabilities TrackCapabilities

	ThresholdsReused []string

	ThresholdsUnavailable []string
}

func (r *TrackResult) NoDataCount() int {
	n := 0
	for _, v := range r.Rows {
		if v.IsNoData() {
			n++
		}
	}
	return n
}

func (r *TrackResult) CountByReason(reason NoDataReason) int {
	n := 0
	for _, v := range r.Rows {
		if v.NoDataReason == reason {
			n++
		}
	}
	return n
}
