package core

import (
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
		cloudSea := "无"
		if _, ok := profile.HighestBeneath(siteAlt, layers); ok {
			low := surface.CloudCoverLow
			if !low.Valid {
				low = profile.MaxCCBelow(levels, 2500.0)
			}
			if low.Valid && low.V >= cloudSeaDeckLowCC {
				cloudSea = "有"
			}
		}
		note := ev.Note
		if nAbove < 2 {
			note += "；机位以上可用气压层不足 2 个，云底/云顶分辨率低，置信度有限"
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
