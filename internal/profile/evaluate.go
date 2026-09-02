package profile

import (
	"strconv"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// 评级结论常量，转发自 model。
const (
	RATING_OK     = model.RATING_OK
	RATING_WARN   = model.RATING_WARN
	RATING_BAD    = model.RATING_BAD
	RATING_NODATA = model.RATING_NODATA

	RATING_CLEAR = model.RATING_CLEAR
)

// Evaluation 是单小时的评估结论：评级、云与机位的关系、给人看的说明，
// 以及决定该结论的关键云层（无云或无数据时为 nil）。
type Evaluation struct {
	Rating   string
	Relation string
	Note     string
	KeyLayer *CloudLayer
}

// EvaluateHour 综合廓线反演结果与地面要素，给出单小时的通透性结论。
//
// 判定顺序：先由云与机位的关系定基准评级，再依次叠加降水、中/高云、雾、
// 模式低云量交叉校验等修正项。修正只会让结论变差不会变好——
// 除降水与雾这类硬否决直接判为不宜外，其余走 model.Worse 取更严重者。
//
// 廓线全缺测时不做通透性判断：只有降水或雾这类不依赖廓线的证据能给出结论，
// 否则一律判为无数据，绝不因为「没看到云」就说通透。
func EvaluateHour(site model.Site, surface model.Surface, layers []CloudLayer,
	levels []Level, t config.Thresholds) Evaluation {

	if len(levels) == 0 {
		notes := make([]string, 0, 6)
		rating := ""
		rating, notes = applyPrecipVeto(surface, rating, notes)
		rating, notes = applyFogVeto(surface, t, rating, notes)
		if rating == "" {
			return Evaluation{
				Rating:   RATING_NODATA,
				Relation: REL_NODATA,
				Note:     "气压层廓线全缺测，无法反演云底/云顶，不做通透性判断",
				KeyLayer: nil,
			}
		}
		return Evaluation{
			Rating:   rating,
			Relation: REL_NODATA,
			Note:     strings.Join(notes, "；"),
			KeyLayer: nil,
		}
	}

	siteAlt := site.Alt
	relation, keyLayer := ClassifySite(siteAlt, layers)

	rh2m := surface.RelativeHumidity2m
	notes := make([]string, 0, 6)
	var rating string

	switch relation {
	case REL_CLEAR:
		rating = RATING_OK
		notes = append(notes, "全层无云，头顶通透")

	case REL_SEA_BELOW:
		rating = RATING_OK
		gap := siteAlt - keyLayer.TopMSL
		if gap <= t.CloudSeaMaxDepthM {
			notes = append(notes, "云海在脚下（云顶低于机位 "+
				model.FormatFixed(gap, 0)+"m），头顶通透，最佳拍摄条件")
		} else {
			// 云顶离机位太远，拍不到云海，只能说「不碍事」而非卖点。
			notes = append(notes, "头顶通透；低云仅存在于机位下方 "+
				model.FormatFixed(gap, 0)+"m 的谷地，不影响拍摄")
		}

	case REL_IN_CLOUD:
		aboveTop := keyLayer.TopMSL - siteAlt
		belowBase := siteAlt - keyLayer.BaseMSL

		// 高山云海形态：机位嵌在云层顶部附近、脚下是厚厚的云海。
		// 几何上机位确实在云里（云顶还在头顶），但主导题材是脚下的云海，
		// 不该一律判🔴——牛草山这类点位常年云海，机位恰好在云层上沿、
		// 头顶只剩薄云，是出片的好时机。必须「脚下云够厚」且「头顶云够薄」同时满足，
		// 否则仍是埋在厚云里。命中后改写关系为专属的 REL_SEA_BELOW_IN_CLOUD，
		// 让报告「主要状态」直接呈现「云海在脚下（机位在云中）」而非笼统的「机位在云中」。
		if belowBase >= t.CloudSeaBeneathDepthM && aboveTop <= t.CloudSeaAboveDepthM {
			relation = REL_SEA_BELOW_IN_CLOUD
			rating = RATING_WARN
			notes = append(notes,
				"云海在脚下（机位在云中）：云底在山脚约 "+
					model.FormatFixed(belowBase, 0)+"m、云顶在头顶约 "+
					model.FormatFixed(aboveTop, 0)+
					"m，机位处在云层顶部附近，是高山云海典型形态；"+
					"可守候云隙破云，但稳定性差、山顶大概率有雾凇/湿雾")
			break
		}

		// 普通「机位埋在云中」：只靠湿度判出来的薄层给风险，成云给不宜。
		// 只靠湿度判出来的层，且近地湿度还没到雾的量级，视为「可能起雾」而非坐实在云里。
		soft := keyLayer.RHOnly(t) && !rh2m.GE(t.FogProxyRHHigh)
		if soft {
			rating = RATING_WARN
			notes = append(notes, "机位处在近饱和湿层（模式云量仅 "+
				model.FormatFixed(keyLayer.MaxCC, 0)+"%），有起雾/低云风险，需现场确认")
		} else {
			rating = RATING_BAD
			notes = append(notes, "机位在云中，无法拍摄（云顶还在头顶 "+
				itoa(model.RoundToInt(aboveTop))+"m）")
		}

	default:
		// 头顶有云：按云量分档定严重程度，湿度判出来的层只给风险。
		baseAGL := model.RoundToInt(keyLayer.BaseMSL - siteAlt)
		var desc string
		switch {
		case keyLayer.RHOnly(t):
			rating = RATING_WARN
			desc = "按湿层判定（模式云量仅 " + model.FormatFixed(keyLayer.MaxCC, 0) + "%），或仅为薄云"
		case keyLayer.MaxCC >= t.OverheadSevereCC:
			rating = RATING_BAD
			desc = "成片遮挡（云量 " + model.FormatFixed(keyLayer.MaxCC, 0) + "%）"
		default:
			rating = RATING_WARN
			desc = "部分遮挡或有云缝（云量 " + model.FormatFixed(keyLayer.MaxCC, 0) + "%）"
		}
		notes = append(notes, "云底在头顶 "+itoa(baseAGL)+"m，"+desc)

		// 头顶有云不妨碍脚下同时有云海，补一句免得漏掉可拍的题材。
		if top, ok := HighestBeneath(siteAlt, layers); ok {
			gap := model.RoundToInt(siteAlt - top)
			notes = append(notes, "脚下另有云海（云顶低于机位 "+itoa(gap)+"m）")
		}
	}

	rating, notes = applyPrecipVeto(surface, rating, notes)

	// 中/高云在气压层剖面（约 3km 以下）之外，只能靠地面模式量补判；
	// 仅在结论仍为通透时检查，已经变差的结论不必再叠加。
	if rating == RATING_OK {

		if mid := surface.CloudCoverMid; mid.GE(t.MidCloudVeilCC) {
			rating = RATING_WARN
			notes = append(notes, "中云量 "+model.FormatFixed(mid.V, 0)+
				"%（3–8km，剖面之外），成片中云盖顶，星野受损")
		}
		if high := surface.CloudCoverHigh; high.GE(t.HighCloudThinVeilCC) {
			rating = RATING_WARN
			notes = append(notes, "高云量 "+model.FormatFixed(high.V, 0)+
				"%（8km 以上卷云），头顶薄卷云，星野略受损")
		}
	}

	rating, notes = applyFogVeto(surface, t, rating, notes)

	// 辐射雾豁免：辐射雾是贴地雾（静风、晴夜辐射冷却形成），
	// 当机位在雾顶之上（脚下云海/云海淹没机位）时，雾会随日出消散，
	// 是可守候的云海/雾海题材，不应被雾 veto 一棍子打成🔴。
	// 仅对 REL_SEA_BELOW_IN_CLOUD（脚下云海）放行；
	// 普通浓雾、机位埋在厚云中仍维持🔴。
	if relation == REL_SEA_BELOW_IN_CLOUD && rating == RATING_BAD && isRadiationFog(surface, t) {
		rating = RATING_WARN
		notes = replaceLastNote(notes, "辐射雾",
			"辐射雾贴地（静风、晴夜辐射冷却形成），清晨随日出消散；"+
				"机位在雾顶之上，脚下为雾海/云海，可守候云隙破云与日出云海")
	}

	// 交叉校验：模式低云量与剖面结论矛盾时降级。
	// 气压层之间的薄云、以及顶到机位的云海，都是剖面看不见的。
	apiLow := surface.CloudCoverLow
	if apiLow.Valid {
		switch {
		case relation == REL_CLEAR && apiLow.V >= t.ProfileLowcloudCrossChk:
			rating = model.Worse(rating, RATING_WARN)
			notes = append(notes, "剖面未见云层但模式低云量 "+
				model.FormatFixed(apiLow.V, 0)+"%，可能有气压层之间的薄云，需谨慎")
		case relation == REL_SEA_BELOW && apiLow.V >= t.CloudSeaSuspectLowcloud:
			rating = model.Worse(rating, RATING_WARN)
			notes = append(notes, "模式低云量 "+
				model.FormatFixed(apiLow.V, 0)+"% 偏高，云顶可能已顶到机位，请复核")
		}
	}

	if keyLayer != nil && keyLayer.OpenTop {
		notes = append(notes, "云顶超出剖面顶(3km)，云厚为下限")
	}

	// 结露与起雾提示：只影响拍摄准备，不改评级。
	// 已经在云里（或云海淹没机位）就没必要再提「可能起雾」了。
	temp, dew := surface.Temperature2m, surface.DewPoint2m
	if relation != REL_IN_CLOUD && relation != REL_SEA_BELOW_IN_CLOUD && temp.Valid && dew.Valid {
		spread := temp.V - dew.V
		if spread < t.DewSpreadC {
			notes = append(notes, "温露差 "+model.FormatFixed(spread, 1)+"℃，镜头结露风险")
		}

		// 由温露差粗估抬升凝结高度（离地米），越低越容易起雾。
		lclAGL := 124.0 * spread
		switch {
		case lclAGL < t.LCLAlertAGLM:
			notes = append(notes, "LCL≈"+itoa(model.RoundToInt(lclAGL))+"m(估算)，辐射雾风险高")
		case lclAGL < t.LCLWarnAGLM:
			notes = append(notes, "LCL≈"+itoa(model.RoundToInt(lclAGL))+"m(估算)，警惕起雾")
		}
	}

	return Evaluation{
		Rating:   rating,
		Relation: relation,
		Note:     strings.Join(notes, "；"),
		KeyLayer: keyLayer,
	}
}

func itoa(v int) string { return strconv.Itoa(v) }

// maxValid 取两个可空值中较大的有效值；只有一个有效时取它，都缺测则返回缺测。
func maxValid(a, b model.OptFloat) model.OptFloat {
	switch {
	case a.Valid && b.Valid:
		if a.V >= b.V {
			return a
		}
		return b
	case a.Valid:
		return a
	case b.Valid:
		return b
	default:
		return model.Missing()
	}
}

// applyPrecipVeto 降水一票否决：只要这小时有降水就直接判为不宜，
// 并把降水量与天气码（雷暴单独点名）写进说明。无降水时原样返回。
func applyPrecipVeto(surface model.Surface, rating string, notes []string) (string, []string) {
	if surface.HasPrecip() {
		rating = RATING_BAD
		parts := make([]string, 0, 2)
		if surface.Precipitation.Valid && surface.Precipitation.V > 0 {
			parts = append(parts, "降水 "+model.FormatFixed(surface.Precipitation.V, 1)+"mm")
		}
		if surface.WeatherCode.Valid && model.IsPrecipCode(int(surface.WeatherCode.V)) {
			code := int(surface.WeatherCode.V)
			if model.IsThunderstormCode(code) {
				parts = append(parts, "雷暴天气（天气码 "+itoa(code)+"）")
			} else {
				parts = append(parts, "降水天气码 "+itoa(code))
			}
		}
		notes = append(notes, strings.Join(parts, "，")+"，不宜拍摄")
	}
	return rating, notes
}

// applyFogVeto 按能见度判雾：低于 FogVisibilityM 直接判不宜，
// 轻雾/霾档只降级为风险（取更严重者，不会把已经不宜的结论洗白）。
//
// 能见度缺测时才退用近地相对湿度作代理判据，其可信度低于能见度。
func applyFogVeto(surface model.Surface, t config.Thresholds, rating string, notes []string) (string, []string) {
	vis := surface.Visibility
	rh2m := surface.RelativeHumidity2m

	if vis.Valid {
		switch {
		case vis.V < t.FogVisibilityM:
			rating = RATING_BAD
			notes = append(notes, "能见度 "+itoa(model.RoundToInt(vis.V))+"m，"+fogKindText(surface, t))
		case vis.V < t.HazeVisibilityM:
			rating = model.Worse(rating, RATING_WARN)
			notes = append(notes, "能见度 "+itoa(model.RoundToInt(vis.V))+"m，轻雾/霾")
		}
	} else if rh2m.Valid {
		switch {
		case rh2m.V >= t.FogProxyRHHigh:
			rating = RATING_BAD
			notes = append(notes, "近地RH "+model.FormatFixed(rh2m.V, 0)+"%(代理判据)，"+fogKindText(surface, t))
		case rh2m.V >= t.FogProxyRHWarn:
			rating = model.Worse(rating, RATING_WARN)
			notes = append(notes, "近地RH "+model.FormatFixed(rh2m.V, 0)+"%(代理判据)，起雾风险")
		}
	}
	return rating, notes
}

// fogKindText 按风速区分雾的成因，直接影响「能不能等它散」的判断。
func fogKindText(surface model.Surface, t config.Thresholds) string {
	wind := surface.WindSpeed10m
	if !wind.Valid {
		return "有雾"
	}
	if wind.V < t.FogCalmWindMS {
		return "辐射雾（静风 " + model.FormatFixed(wind.V, 1) + "m/s，天亮前最重）"
	}
	return "平流雾/低云压顶（风 " + model.FormatFixed(wind.V, 1) + "m/s）"
}

// isRadiationFog 判断是否为辐射雾：静风（同 fogKindText 的判定）且中/高云不厚，
// 即晴夜地面辐射冷却形成的贴地雾。中高云偏多会抑制辐射冷却，倾向平流/层云雾；
// 中高云缺测时不排除，保守保留辐射雾可能。
func isRadiationFog(surface model.Surface, t config.Thresholds) bool {
	wind := surface.WindSpeed10m
	if !wind.Valid || wind.V >= t.FogCalmWindMS {
		return false
	}
	if c := surface.CloudCoverMid; c.Valid && c.V >= t.RadiationFogMidHighCC {
		return false
	}
	if c := surface.CloudCoverHigh; c.Valid && c.V >= t.RadiationFogMidHighCC {
		return false
	}
	return true
}

// replaceLastNote 把 notes 中最后一句含 needle 的说明替换为 replacement；
// 找不到时直接追加。用于雾 veto 降级后，把辐射雾的消极措辞改写为积极指引。
func replaceLastNote(notes []string, needle, replacement string) []string {
	for i := len(notes) - 1; i >= 0; i-- {
		if strings.Contains(notes[i], needle) {
			notes[i] = replacement
			return notes
		}
	}
	return append(notes, replacement)
}
