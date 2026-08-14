package dualtrack

import (
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

var bTrackReusedThresholds = []string{
	"FogVisibilityM",
	"HazeVisibilityM",
	"FogProxyRHHigh",
	"FogProxyRHWarn",
	"FogCalmWindMS",
	"LCLAlertAGLM",
}

var bTrackUnavailableThresholds = []string{
	"RHThresholdLow",
	"RHThresholdHigh",
	"RHLowLayerPressureMin",
	"LayerMinHalfSpanFrac",
	"MinLevelHeightMSL",
	"CloudSeaMaxDepthM",
	"ProfileLowcloudCrossChk",
	"CloudSeaSuspectLowcloud",
	"DewSpreadC",
}

func Assemble(
	siteID string,
	utcOffsetHours float64,
	siteAlt float64,
	rows []model.HourRow,
	samples []HourInput,
	quotaOK bool,
	datum string,
	th *config.Thresholds,
) (*TrackResult, error) {

	if err := RequireAGLDatum(datum); err != nil {
		return nil, err
	}

	res := &TrackResult{
		SiteID: siteID,

		Active:         quotaOK && len(samples) > 0,
		QuotaExhausted: !quotaOK,
		Rows:           make([]HourVerdict, len(rows)),
		Capabilities: TrackCapabilities{

			HasCloudTopData: false,
			SeaBelowUnknown: true,
		},
		ThresholdsReused:      append([]string(nil), bTrackReusedThresholds...),
		ThresholdsUnavailable: append([]string(nil), bTrackUnavailableThresholds...),
	}

	offset := time.Duration(utcOffsetHours * float64(time.Hour))

	index := make(map[int64]int, len(samples))
	var horizon time.Time
	for i, s := range samples {
		utc := s.TimeUTC.UTC()

		if _, dup := index[utc.Unix()]; !dup {
			index[utc.Unix()] = i
		}
		if horizon.IsZero() || utc.After(horizon) {
			horizon = utc
		}
	}

	for i, r := range rows {
		rowUTC := r.Time.Add(-offset).UTC()
		res.Rows[i] = assembleRow(rowUTC, r.Time, siteAlt, samples, index,
			horizon, quotaOK, th)
	}
	return res, nil
}

func assembleRow(
	rowUTC time.Time,
	timeLocal time.Time,
	siteAlt float64,
	samples []HourInput,
	index map[int64]int,
	horizon time.Time,
	quotaOK bool,
	th *config.Thresholds,
) HourVerdict {

	if !quotaOK {
		return noDataRow(timeLocal, rowUTC, RoundQuotaDown,
			"本轮 Tomorrow.io 配额已耗尽，未发起任何请求；配额恢复后重跑即可获得 B 轨结果")
	}

	if !horizon.IsZero() && rowUTC.After(horizon) {
		return noDataRow(timeLocal, rowUTC, OutOfHorizon,
			"Tomorrow 预报上限 5 天（120 小时），此夜问不到，且明日也不会有"+
				"——除非该夜进入 120 小时窗内")
	}

	idx, ok := index[rowUTC.Unix()]
	if !ok {
		return noDataRow(timeLocal, rowUTC, KeyMissing,
			"预报窗内缺少该时次样本，本时次无从判断（不按晴空处理）")
	}

	v := RateHour(samples[idx], siteAlt, th)

	v.TimeLocal = timeLocal
	v.TimeUTC = rowUTC
	return v
}

func noDataRow(timeLocal, timeUTC time.Time, reason NoDataReason,
	note string) HourVerdict {

	return HourVerdict{
		TimeLocal:       timeLocal,
		TimeUTC:         timeUTC,
		Rel:             model.REL_NODATA,
		Rating:          model.RATING_NODATA,
		NoDataReason:    reason,
		TerrainFidelity: TerrainUnknown,
		SeaBelowUnknown: true,
		Note:            note,
	}
}
