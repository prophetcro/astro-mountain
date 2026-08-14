package tomorrow

import (
	"fmt"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
)

func ToDualTrackInputs(sr *SiteResult) ([]dualtrack.HourInput, error) {
	if sr == nil {
		return nil, fmt.Errorf("tomorrow: SiteResult 为 nil")
	}
	if len(sr.CloudBaseAGL) != len(sr.Samples) {
		return nil, fmt.Errorf("%w：samples=%d cloudBaseAGL=%d",
			dualtrack.ErrSeriesMalformed, len(sr.Samples), len(sr.CloudBaseAGL))
	}

	out := make([]dualtrack.HourInput, len(sr.Samples))
	for i, s := range sr.Samples {
		out[i] = dualtrack.HourInput{
			TimeUTC: s.TimeUTC,

			CloudBaseAGLM:        sr.CloudBaseAGL[i],
			CloudCover:           s.CloudCover,
			VisibilityKm:         s.VisibilityKm,
			HumidityPct:          s.HumidityPct,
			WindSpeedMS:          s.WindSpeedMS,
			PrecipProbabilityPct: s.PrecipProbabilityPct,

			PressureSurfaceHPa: s.PressureSurfaceHPa,
			PressureSeaHPa:     s.PressureSeaHPa,
		}
	}
	return out, nil
}

func RateSite(sr *SiteResult, datum Datum,
	th *config.Thresholds) ([]dualtrack.HourVerdict, error) {

	inputs, err := ToDualTrackInputs(sr)
	if err != nil {
		return nil, err
	}
	return dualtrack.RateSeries(inputs, sr.Site.Alt, string(datum), th)
}
