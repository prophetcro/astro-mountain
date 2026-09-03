package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/astro"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

// cloudSeaDeckLowCC 判定「有无云海」时要求的最低低云量（%）：
// 机位下方存在云层顶（几何）之外，还需足够云量形成连续云面，避免零散积云误报为云海。
const cloudSeaDeckLowCC = 40.0

// InNightWindow 判断某个整点是否落在夜间窗口内。
// 窗口跨零点（如 19:00 → 次日 05:00），故用「或」而非「与」。
func InNightWindow(hour int, w config.WindowConfig) bool {
	return hour >= w.NightStartHour || hour <= w.NightEndHour
}

// InCoreWindow 判断某个整点是否落在核心观测窗口内（夜间窗口的收窄子集）。
// 同样跨零点。
func InCoreWindow(hour int, w config.WindowConfig) bool {
	return hour >= w.CoreStartHour || hour <= w.CoreEndHour
}

// NightIDOf 返回某个本地时刻所属的「观测夜」ID（YYYY-MM-DD）。
// 回拨 12 小时再取日期，于是次日凌晨的时刻仍归到前一天那一夜。
func NightIDOf(dt time.Time) string {
	return dt.Add(-12 * time.Hour).Format("2006-01-02")
}

// AnalyseSite 把单个站点的原始预报响应转换成逐小时评估行。
//
// 只保留落在夜间窗口内的时刻；targetNights 非 nil 时进一步只保留列表中的夜。
// 廓线不可用的时刻不会被丢弃，而是产出一行显式的无数据记录，
// 让报告如实呈现「这个钟点没有数据」而不是留空。
func AnalyseSite(site Site, resp *api.Response, targetNights map[string]bool,
	cfg config.Config) []HourRow {

	siteAlt := site.Alt
	rows := make([]HourRow, 0, 16)

	for idx, localDT := range resp.Times {
		if !InNightWindow(localDT.Hour(), cfg.Window) {
			continue
		}
		night := NightIDOf(localDT)
		if targetNights != nil && !targetNights[night] {
			continue
		}

		surface := resp.Surface(idx)
		levelValues := resp.LevelValues(idx)
		levels := profile.BuildProfile(levelValues, cfg.Thresh)
		info := astro.Compute(localDT, resp.UTCOffsetSeconds,
			site.Lat, site.Lon, cfg.Thresh.AstroDarkSunAlt)

		if !ProfileUsable(levels) {
			rows = append(rows, nodataRow(site, localDT, night, info))
			continue
		}

		layers := profile.DetectLayers(levels, cfg.Thresh)
		ev := profile.EvaluateHour(site, surface, layers, levels, cfg.Thresh)
		nAbove := profile.CountAbove(levels, siteAlt)

		// 有无云海：机位下方存在一层云（几何成立）且低云量足以形成连续云面。
		// 与「主要状态/主要诱因」解耦——即便山顶起雾或降水把云海遮住，几何上有就标「有」。
		// 与云海时段检测、逐小时评级共用同一份几何判定（profile.ClassifySeaGeometry），
		// 否则会出现「主要状态=云海在脚下」却「云海=无」的自相矛盾——
		// 淹没型云海（云堆过机位、脚下无独立层）在旧口径下就被判成「无」。
		cloudSea := "无"
		cloudSeaForm := ""
		if cls := profile.ClassifySeaGeometry(siteAlt, layers, cfg.Thresh); cls.Present {
			low := surface.CloudCoverLow
			if !low.Valid {
				low = profile.MaxCCBelow(levels, 2500.0)
			}
			if low.Valid && low.V >= cloudSeaDeckLowCC {
				cloudSea = "有"
				if cls.Kind == profile.SEA_SUBMERGED {
					cloudSeaForm = "淹没型"
				} else {
					cloudSeaForm = "脚下型"
				}
			}
		}
		note := ev.Note
		if nAbove < 2 {
			note += "；机位以上可用气压层不足 2 个，云底/云顶分辨率低，置信度有限"
		}
		// 2026-09 模型缺层降级：覆盖机位的相邻层间距 > 500m 时（典型为 ECMWF/JMA 只给 4 层），
		// 显式标注「模式垂直分辨率不足，云海判定置信度有限」。
		// 这条比「无云海」更诚实——用户最恨静默失败。
		if gap := profile.MaxGapAroundSite(levels, siteAlt); gap > 500 {
			note += "；模式垂直分辨率不足（机位上下相邻层间距 " +
				model.FormatFixed(gap, 0) + "m），云海判定置信度有限"
		}

		// 云海成因加权：几何上判出云海后，用四要素给可信度加权并写入说明。
		// 几何判定（机位下方是否有连续云面）仍是云海有无的唯一权威，这里只负责
		// 在「已判为有」时补成因可信度——成因齐备则高置信（典型云海条件），
		// 仅几何成立但成因缺失则低置信（可能是零散低云、云面不稳定）。
		if cloudSea == "有" {
			prev := prevNightPrecipMM(resp, idx, cfg)
			cause := assessCloudSeaCause(surface, ev.Relation, prev, cfg.Thresh)
			note += "；" + cloudSeaCauseNote(cause)
		}

		// 低云量优先用 API 直给值；缺测时退而用廓线中 2500 m 以下的最大云量，
		// CloudLowSource 记录来源，报告与下游据此判断可信度。
		cloudLow := surface.CloudCoverLow
		cloudLowSrc := "api"
		if !cloudLow.Valid {
			cloudLow = profile.MaxCCBelow(levels, 2500.0)
			cloudLowSrc = "profile"
		}

		spread := SafeSpread(surface.Temperature2m, surface.DewPoint2m)

		row := HourRow{
			Site:      site.Name,
			Lat:       site.Lat,
			Lon:       site.Lon,
			Alt:       site.Alt,
			TimeISO:   localDT.Format("2006-01-02T15:04"),
			TimeShort: localDT.Format("01-02 15:04"),
			Hour:      localDT.Hour(),
			Night:     night,
			HasData:   true,
			Time:      localDT,

			LevelsTotal: len(levels),
			LevelsAbove: nAbove,

			Relation: model.Str(ev.Relation),
			Rating:   ev.Rating,
			Note:     note,

			CloudSea: cloudSea,

			CloudSeaForm: cloudSeaForm,

			CloudLow:       model.RoundOpt(cloudLow, 0),
			CloudLowSource: model.Str(cloudLowSrc),
			CloudMid:       model.RoundOpt(surface.CloudCoverMid, 0),
			CloudHigh:      model.RoundOpt(surface.CloudCoverHigh, 0),

			Visibility: model.RoundOpt(surface.Visibility, 0),
			Temp:       model.RoundOpt(surface.Temperature2m, 1),
			Dew:        model.RoundOpt(surface.DewPoint2m, 1),
			Spread:     model.RoundOpt(spread, 1),
			WindMS:     model.RoundOpt(surface.WindSpeed10m, 1),

			BoundaryLayerHeight: model.RoundOpt(surface.BoundaryLayerHeight, 0),
			FreezingLevelHeight: model.RoundOpt(surface.FreezingLevelHeight, 0),
			LCLAGLEst:           model.RoundOpt(SafeLCL(spread), 0),

			SunAlt:    model.Num(model.Round(info.SunAlt, 1)),
			MoonAlt:   model.Num(model.Round(info.MoonAlt, 1)),
			MoonIllum: model.Num(model.RoundPy(info.MoonIllum*100.0, 0)),
			MoonPhase: info.MoonPhaseName,
			GCAlt:     model.Num(model.Round(info.GCAlt, 1)),
			AstroDark: info.AstroDark,

			Layers: layerInfos(layers, cfg.Thresh),
		}

		// 只有评估选出了关键云层，云底/云顶等字段才有值；否则保持缺测。
		// AGL 一律由 MSL 减去机位海拔得到，可能为负（云底低于机位，即脚下云海）。
		if ev.KeyLayer != nil {
			kl := ev.KeyLayer
			row.CloudBaseMSL = model.Num(model.RoundPy(kl.BaseMSL, 0))
			row.CloudBaseAGL = model.Num(model.RoundPy(kl.BaseMSL-siteAlt, 0))
			row.CloudTopMSL = model.Num(model.RoundPy(kl.TopMSL, 0))
			row.CloudTopAGL = model.Num(model.RoundPy(kl.TopMSL-siteAlt, 0))
			row.CloudThickness = model.Num(model.RoundPy(kl.Thickness(), 0))
			row.LayerMaxCC = model.Num(model.RoundPy(kl.MaxCC, 0))
		}

		rows = append(rows, row)
	}
	return rows
}

// layerInfos 把内部云层结构转换成可序列化的 LayerInfo 列表，
// 数值统一取整后再出包，保证报告与 JSON 导出看到的是同一组数。
func layerInfos(layers []profile.CloudLayer, t config.Thresholds) []model.LayerInfo {

	out := make([]model.LayerInfo, 0, len(layers))
	for _, lv := range layers {
		out = append(out, model.LayerInfo{
			BaseMSL:   model.Num(model.RoundPy(lv.BaseMSL, 0)),
			TopMSL:    model.Num(model.RoundPy(lv.TopMSL, 0)),
			Thickness: model.Num(model.RoundPy(lv.Thickness(), 0)),
			MaxCC:     model.Num(model.RoundPy(lv.MaxCC, 0)),
			MaxRH:     model.Num(model.RoundPy(lv.MaxRH, 0)),
			OpenTop:   lv.OpenTop,
			OpenBase:  lv.OpenBase,
			RHOnly:    lv.RHOnly(t),
		})
	}
	return out
}

// nodataRow 构造一行无气象数据的记录：气象字段全部留缺测，
// 评级固定为 RATING_NODATA（缺测安全红线，见 AssertNoDataRow），
// 但天文量仍然照常填充——它们由本地算法算出，不依赖预报数据。
func nodataRow(site Site, localDT time.Time, night string, info astro.Info) HourRow {
	return HourRow{
		Site:      site.Name,
		Lat:       site.Lat,
		Lon:       site.Lon,
		Alt:       site.Alt,
		TimeISO:   localDT.Format("2006-01-02T15:04"),
		TimeShort: localDT.Format("01-02 15:04"),
		Hour:      localDT.Hour(),
		Night:     night,
		HasData:   false,
		Time:      localDT,

		LevelsTotal: 0,
		LevelsAbove: 0,

		Relation: model.NullStr(),
		Rating:   RATING_NODATA,
		Note:     "超出该模式预报时效或缺测，无气象数据",

		CloudLowSource: model.NullStr(),

		SunAlt:    model.Num(model.Round(info.SunAlt, 1)),
		MoonAlt:   model.Num(model.Round(info.MoonAlt, 1)),
		MoonIllum: model.Num(model.Round(info.MoonIllum*100.0, 0)),
		MoonPhase: info.MoonPhaseName,
		GCAlt:     model.Num(model.Round(info.GCAlt, 1)),
		AstroDark: info.AstroDark,

		Layers: []model.LayerInfo{},
	}
}

// cloudSeaCause 汇总云海四要素的命中情况，给几何判定出的云海加权可信度。
//
// 四要素（用户给定的云海必要条件）：前晚下雨、第二天转晴、风力≤3级、有逆温层。
// Score 为命中的成因数（0-4），Confidence 据此映射到 高/中/低。
type cloudSeaCause struct {
	PrevNightPrecipMM float64
	HasRain           bool   // 前晚累计降水 >= 阈值
	Cleared           bool   // 第二天转晴：机位上方通透（无云压顶）
	WindCalm          bool   // 风力 ≤ 静风门槛(3级)
	InversionLikely   bool   // 边界层高度低 → 逆温（稳定层结）可能
	Score             int    // 命中的成因数（0-4）
	Confidence        string // 高 / 中 / 低
}

// assessCloudSeaCause 根据当前小时地面要素与前晚降水量评估云海成因。
func assessCloudSeaCause(surface model.Surface, relation string, prevPrecipMM float64, t config.Thresholds) cloudSeaCause {
	c := cloudSeaCause{PrevNightPrecipMM: prevPrecipMM}
	c.HasRain = prevPrecipMM >= t.CloudSeaPrevNightPrecipMM
	c.Cleared = relation == model.REL_SEA_BELOW || relation == model.REL_SEA_BELOW_IN_CLOUD
	if w := surface.WindSpeed10m; w.Valid {
		c.WindCalm = w.V <= t.CloudSeaCalmWindMS
	}
	if blh := surface.BoundaryLayerHeight; blh.Valid {
		c.InversionLikely = blh.V < t.CloudSeaInversionBLHM
	}
	c.Score = countTrue(c.HasRain, c.Cleared, c.WindCalm, c.InversionLikely)
	c.Confidence = cloudSeaConfidence(c.Score)
	return c
}

func countTrue(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

func cloudSeaConfidence(score int) string {
	switch {
	case score >= 4:
		return "高"
	case score >= 2:
		return "中"
	default:
		return "低"
	}
}

// cloudSeaCauseNote 把云海成因渲染成人话，写入「主要诱因」列。
func cloudSeaCauseNote(c cloudSeaCause) string {
	parts := make([]string, 0, 4)
	if c.HasRain {
		parts = append(parts, fmt.Sprintf("前晚降水%.1fmm✓", c.PrevNightPrecipMM))
	} else {
		parts = append(parts, "前晚无降水✗")
	}
	if c.WindCalm {
		parts = append(parts, "静风(≤3级)✓")
	} else {
		parts = append(parts, "风力偏大✗")
	}
	if c.InversionLikely {
		parts = append(parts, "逆温(边界层低)✓")
	} else {
		parts = append(parts, "无明显逆温✗")
	}
	if c.Cleared {
		parts = append(parts, "头顶通透✓")
	} else {
		parts = append(parts, "头顶有云✗")
	}
	label := map[string]string{
		"高": "高置信·典型云海条件",
		"中": "中置信",
		"低": "低置信·成因不足（可能为零散低云/云面不稳定）",
	}[c.Confidence]
	return "云海成因：" + strings.Join(parts, "、") + "，" + label
}

// prevNightPrecipMM 累计「前晚」降水量（mm）作为「前晚下雨」成因的代理。
//
// 统计范围：前一个观测夜的全部夜间小时 + 同夜但早于当前时刻的夜间小时。
// 若数据窗口不含前一个观测夜（如只预报单夜），则退化为「入夜以来到当前时刻」的累计，
// 仍能量化「拍摄前下过雨」这一成因。
func prevNightPrecipMM(resp *api.Response, idx int, cfg config.Config) float64 {
	if idx < 0 || idx >= len(resp.Times) {
		return 0
	}
	cur := resp.Times[idx]
	curNight := NightIDOf(cur)
	prevNight := prevNightID(curNight)
	sum := 0.0
	for j, dt := range resp.Times {
		if j == idx {
			continue
		}
		if !InNightWindow(dt.Hour(), cfg.Window) {
			continue
		}
		n := NightIDOf(dt)
		count := n == curNight && dt.Before(cur) || n == prevNight
		if !count {
			continue
		}
		if p := resp.Surface(j).Precipitation; p.Valid {
			sum += p.V
		}
	}
	return sum
}

func prevNightID(curNight string) string {
	t, err := time.Parse("2006-01-02", curNight)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, -1).Format("2006-01-02")
}
