package dualtrack

import (
	"math"

	"github.com/prophetcro/astro-mountain/internal/model"
)

const (
	barometricScaleM = 44330.0

	barometricExp = 1.0 / 5.255
)

func HModel(surfaceHPa, seaHPa float64) float64 {
	if !isFinite(surfaceHPa) || !isFinite(seaHPa) {
		return math.NaN()
	}
	if seaHPa <= 0 || surfaceHPa < 0 {
		return math.NaN()
	}
	return barometricScaleM * (1.0 - math.Pow(surfaceHPa/seaHPa, barometricExp))
}

func HModelOpt(surfaceHPa, seaHPa model.OptFloat) model.OptFloat {
	if !surfaceHPa.Valid || !seaHPa.Valid {
		return model.Missing()
	}
	return model.NumOrMissing(HModel(surfaceHPa.V, seaHPa.V))
}

func SeriesHModel(surfaceHPa, seaHPa []model.OptFloat) []model.OptFloat {
	if len(surfaceHPa) != len(seaHPa) {
		return nil
	}
	out := make([]model.OptFloat, len(surfaceHPa))
	for i := range surfaceHPa {
		out[i] = HModelOpt(surfaceHPa[i], seaHPa[i])
	}
	return out
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
