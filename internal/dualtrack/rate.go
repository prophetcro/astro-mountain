package dualtrack

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

const (
	TomorrowNullCloudCoverGate = 40.0

	TomorrowPrecipProbGate = 50.0
)

type HourInput struct {
	TimeUTC time.Time

	CloudBaseAGLM model.OptFloat

	CloudCover model.OptFloat

	VisibilityKm model.OptFloat

	HumidityPct model.OptFloat

	WindSpeedMS model.OptFloat

	PrecipProbabilityPct model.OptFloat

	PressureSurfaceHPa model.OptFloat
	PressureSeaHPa     model.OptFloat
}

const DatumAGL = "agl"

var ErrDatumNotAGL = errors.New("dualtrack: 云底高度基准不是 AGL，B 轨地形订正链不成立")

var ErrSeriesMalformed = errors.New("dualtrack: 逐时样本数与云底数组长度不一致")

func RequireAGLDatum(datum string) error {
	norm := strings.ToLower(strings.TrimSpace(datum))
	if norm == DatumAGL {
		return nil
	}
	if norm == "" {
		return fmt.Errorf("%w：收到空基准，说明调用方没有先经 tomorrow.ParseDatum "+
			"归一化；请传 string(ParseDatum(cfg.API.TomorrowCloudBaseDatum)) 的结果",
			ErrDatumNotAGL)
	}
	return fmt.Errorf("%w：当前为 %q。msl 基准下换算层返回的\"AGL\"已经减过一次"+
		"机位海拔，再走 D6.3 会重复扣减，且结果量级正常、无法从数值上发现；"+
		"请把 api.tomorrow_cloud_base_datum 改回 agl",
		ErrDatumNotAGL, datum)
}

func RateSeries(in []HourInput, siteAlt float64, datum string,
	th *config.Thresholds) ([]HourVerdict, error) {

	if err := RequireAGLDatum(datum); err != nil {
		return nil, err
	}
	out := make([]HourVerdict, len(in))
	for i, h := range in {
		out[i] = RateHour(h, siteAlt, th)
	}
	return out, nil
}

func RateHour(in HourInput, siteAlt float64, th *config.Thresholds) HourVerdict {
	t := thresholdsOrDefault(th)

	v := HourVerdict{
		TimeUTC:         in.TimeUTC,
		Rel:             model.REL_NODATA,
		Rating:          model.RATING_NODATA,
		NoDataReason:    KeyMissing,
		TerrainFidelity: TerrainUnknown,
		CloudBaseAGLM:   in.CloudBaseAGLM,

		SeaBelowUnknown: true,
	}

	if !in.CloudBaseAGLM.Valid {
		return rateNullCloudBase(in, v, t)
	}

	hModel := HModelOpt(in.PressureSurfaceHPa, in.PressureSeaHPa)
	if !hModel.Valid {
		v.Note = "本时次地面气压或海平面气压缺测，无法反解模式地形高度（H_model），" +
			"云底相对机位的高度无从计算；按逐时次独立原则不借邻近时次回填"
		return v
	}
	v.HModelM = hModel

	deltaH := hModel.V - siteAlt
	v.DeltaH = model.Num(deltaH)
	v.TerrainFidelity = ClassifyTerrainFidelity(deltaH)

	above := hModel.V + in.CloudBaseAGLM.V - siteAlt
	v.CloudBaseAboveSite = model.Num(above)

	if in.CloudBaseAGLM.V < 0 {
		v.Rel = model.REL_NODATA
		v.Rating = model.RATING_NODATA
		v.NoDataReason = SemanticFailure
		v.Note = "云底相对模式地形高度为 " + model.FormatFixed(in.CloudBaseAGLM.V, 1) +
			" 米（负值），与该量恒非负的定义矛盾，通常是上游取数基准接错，本时次判语义失效"
		return v
	}

	if in.CloudBaseAGLM.V == 0 {
		return rateGroundFog(in, v, t, above)
	}
	return rateCloudAboveModelGround(in, v, t, above)
}

func rateNullCloudBase(in HourInput, v HourVerdict, t config.Thresholds) HourVerdict {
	switch {
	case !in.CloudCover.Valid:
		v.Rel = model.REL_NODATA
		v.Rating = model.RATING_NODATA
		v.NoDataReason = KeyMissing
		v.Note = "云底与云量双双缺测，本时次无从判断（不按晴空处理）"
		return v

	case in.CloudCover.V >= TomorrowNullCloudCoverGate:

		v.Rel = model.REL_NODATA
		v.Rating = model.RATING_NODATA
		v.NoDataReason = SemanticFailure
		v.Note = "云底为 null（数据源语义=没云）却同时报云量 " +
			model.FormatFixed(in.CloudCover.V, 0) + "%，两者自相矛盾，本时次判语义失效"
		return v
	}

	v.Rel = model.REL_CLEAR
	v.Rating = model.RATING_OK
	v.NoDataReason = NoDataNone

	notes := []string{"云底为 null（数据源语义=没云，云量 " +
		model.FormatFixed(in.CloudCover.V, 0) + "%），头顶通透"}
	v.Rating, notes = clearOverlay(in, v.Rating, notes, t)

	notes = append(notes, "脚下是否有云海 B 轨不可判（无云顶字段），以 A 轨为准")
	v.Note = strings.Join(notes, "；")
	return v
}

func rateGroundFog(in HourInput, v HourVerdict, t config.Thresholds,
	above float64) HourVerdict {

	switch {
	case math.Abs(above) <= TerrainFaithfulMaxM:

		v.Rel = model.REL_OVERHEAD
		v.Rating = model.RATING_BAD
		v.NoDataReason = NoDataNone
		notes := []string{"接地雾(能见度极差)：模式地面云底为 0，且模式地形与机位高差仅 " +
			model.FormatFixed(math.Abs(above), 0) + "m，雾层就在机位高度"}
		v.Rating, notes = clearOverlay(in, v.Rating, notes, t)
		v.Note = strings.Join(notes, "；")

	case above < -TerrainFaithfulMaxM:

		v.Rel = model.REL_BASE_BELOW_UNKNOWN
		v.Rating = model.RATING_NODATA
		v.NoDataReason = AmbiguousBase
		v.Note = "模式地面有雾，但模式地形比机位低 " +
			model.FormatFixed(-above, 0) + "m，" +
			"B 轨无云顶字段，无法判定机位在雾上(云海)还是雾中，本时次不做通透性判断"

	default:

		v.Rel = model.REL_OVERHEAD
		v.Rating = model.RATING_WARN
		v.NoDataReason = NoDataNone
		notes := []string{"上方有低云/雾层：模式地形比机位高 " +
			model.FormatFixed(above, 0) + "m，模式地面的雾层位于机位上方"}
		v.Rating, notes = clearOverlay(in, v.Rating, notes, t)
		v.Note = strings.Join(notes, "；")
	}
	return v
}

func rateCloudAboveModelGround(in HourInput, v HourVerdict,
	t config.Thresholds, above float64) HourVerdict {

	switch {
	case above <= 0:

		v.Rel = model.REL_BASE_BELOW_UNKNOWN
		v.Rating = model.RATING_NODATA
		v.NoDataReason = AmbiguousBase
		v.Note = "云底低于机位 " + model.FormatFixed(-above, 0) + "m：" +
			"B 轨无云顶字段，无法区分「云海在脚下」与「机位埋在云中」，" +
			"本时次不做通透性判断（云海判定以 A 轨的气压层廓线为准）"

	case above <= t.LCLAlertAGLM:
		v.Rel = model.REL_OVERHEAD
		v.Rating = model.RATING_BAD
		v.NoDataReason = NoDataNone
		notes := []string{"低云底 " + itoa(model.RoundToInt(above)) +
			"m，随时可能罩住机位"}
		v.Rating, notes = clearOverlay(in, v.Rating, notes, t)
		v.Note = strings.Join(notes, "；")

	default:
		v.Rel = model.REL_OVERHEAD
		v.Rating = model.RATING_OK
		v.NoDataReason = NoDataNone
		notes := []string{"云底在头顶 " + itoa(model.RoundToInt(above)) + "m（地形订正后）"}
		v.Rating, notes = clearOverlay(in, v.Rating, notes, t)
		notes = append(notes, "脚下是否有云海 B 轨不可判（无云顶字段），以 A 轨为准")
		v.Note = strings.Join(notes, "；")
	}
	return v
}

func clearOverlay(in HourInput, rating string, notes []string,
	t config.Thresholds) (string, []string) {

	visM := model.Missing()
	if in.VisibilityKm.Valid {
		visM = model.Num(in.VisibilityKm.V * 1000.0)
	}

	switch {
	case visM.Valid:
		switch {
		case visM.V < t.FogVisibilityM:
			rating = model.Worse(rating, model.RATING_BAD)
			notes = append(notes, "能见度 "+itoa(model.RoundToInt(visM.V))+"m，"+fogKind(in, t))
		case visM.V < t.HazeVisibilityM:
			rating = model.Worse(rating, model.RATING_WARN)
			notes = append(notes, "能见度 "+itoa(model.RoundToInt(visM.V))+"m，轻雾/霾")
		}

	case in.HumidityPct.Valid:

		switch {
		case in.HumidityPct.V >= t.FogProxyRHHigh:
			rating = model.Worse(rating, model.RATING_BAD)
			notes = append(notes, "近地RH "+model.FormatFixed(in.HumidityPct.V, 0)+
				"%(代理判据)，"+fogKind(in, t))
		case in.HumidityPct.V >= t.FogProxyRHWarn:
			rating = model.Worse(rating, model.RATING_WARN)
			notes = append(notes, "近地RH "+model.FormatFixed(in.HumidityPct.V, 0)+
				"%(代理判据)，起雾风险")
		}
	}

	if in.PrecipProbabilityPct.GE(TomorrowPrecipProbGate) {
		notes = append(notes, "降水概率 "+
			model.FormatFixed(in.PrecipProbabilityPct.V, 0)+"%（仅提示，不改评级）")
	}
	return rating, notes
}

func fogKind(in HourInput, t config.Thresholds) string {
	if !in.WindSpeedMS.Valid {
		return "有雾"
	}
	if in.WindSpeedMS.V < t.FogCalmWindMS {
		return "辐射雾（静风 " + model.FormatFixed(in.WindSpeedMS.V, 1) + "m/s，天亮前最重）"
	}
	return "平流雾/低云压顶（风 " + model.FormatFixed(in.WindSpeedMS.V, 1) + "m/s）"
}

func thresholdsOrDefault(th *config.Thresholds) config.Thresholds {
	if th != nil {
		return *th
	}
	return config.Default().Thresh
}

func itoa(v int) string { return strconv.Itoa(v) }
