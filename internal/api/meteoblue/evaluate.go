package meteoblue

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/astro"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

const timeLayout = "2006-01-02T15:04"

// C 轨 Relation 占位值：Meteoblue 无气压层廓线，不能判定云海几何关系，
// 因此不采用 A 轨的 "云海在脚下/机位在云中/头顶通透" 分类。填一个有效非空
// 字符串既满足 safety.AuditRows 的护栏，也向用户诚实说明该列不适用。
const relationMeteoblueNoGeometry = "Meteoblue 不反演云海几何"

// Meteoblue 雾概率（fog_probability）判级阈值，与降水概率口径一致。
const (
	meteoFogProbWarnPct = 30.0 // 雾概率 ≥ 此值：WARN（警惕起雾）
	meteoFogProbBadPct  = 50.0 // 雾概率 ≥ 此值：硬否决 BAD（不宜拍摄）
)

// EvaluateResponse 把 Meteoblue 响应转换成逐小时评估行。
// 只保留 [start,end] 内、落夜间窗口、且属于 targetNights 的时刻。
// 取数块由 resp.DataBlockOf() 自动选定（免费档 data_3h / 付费档 data_1h）。
func EvaluateResponse(site model.Site, resp *MetoResponse, start, end time.Time,
	targetNights map[string]bool, cfg *config.Config, loc *time.Location) []model.HourRow {

	rows := make([]model.HourRow, 0, 16)
	d := resp.DataBlockOf()
	if d == nil || len(d.Time) == 0 {
		return rows
	}
	for i, tStr := range d.Time {
		localDT, err := parseMetoTime(tStr, loc)
		if err != nil {
			continue
		}
		if localDT.Before(start) || localDT.After(end) {
			continue
		}
		if !inNightWindow(localDT.Hour(), cfg.Window) {
			continue
		}
		night := nightIDOf(localDT)
		if targetNights != nil && !targetNights[night] {
			continue
		}
		rows = append(rows, evaluateHour(site, i, d, cfg, localDT, loc, night))
	}
	return rows
}

// evaluateHour 评估单小时：Meteoblue 不反演云底/云顶（无气压层），
// 仅按分层云量 + 降水 + 能见度判定通透；Relation 填固定占位说明（云海几何不可判）。
func evaluateHour(site model.Site, i int, d *DataBlock, cfg *config.Config,
	localDT time.Time, loc *time.Location, night string) model.HourRow {

	th := &cfg.Thresh

	precip, hasPrecip := fval(d.Precipitation, i)
	precipProb, hasProb := fval(d.PrecipProbability, i)
	low, hasLow := fval(d.LowClouds, i)
	mid, hasMid := fval(d.MidClouds, i)
	high, hasHigh := fval(d.HighClouds, i)
	vis, hasVis := fval(d.Visibility, i)
	fogProb, hasFog := fval(d.FogProbability, i)
	temp, hasTemp := fval(d.Temperature, i)
	rh, hasRH := fval(d.RelativeHumidity, i)
	wind, hasWind := fval(d.WindSpeed10m, i)

	rating := model.RATING_OK
	notes := []string{"Meteoblue（分层云量，不反演云海几何）"}

	// 降水硬否决：有量或概率偏高即判不宜。
	precipBad := (hasPrecip && precip > 0) || (hasProb && precipProb >= 50)
	if precipBad {
		rating = model.RATING_BAD
		parts := make([]string, 0, 2)
		if hasPrecip && precip > 0 {
			parts = append(parts, fmt.Sprintf("降水 %.1fmm", precip))
		}
		if hasProb && precipProb >= 50 {
			parts = append(parts, fmt.Sprintf("降水概率 %.0f%%", precipProb))
		}
		notes = append(notes, strings.Join(parts, "，")+"，不宜拍摄")
	}

	// 低云（<3km，覆盖机位高度）：成片遮挡或偏高压云。
	if hasLow {
		if low >= th.OverheadSevereCC {
			rating = model.Worse(rating, model.RATING_BAD)
			notes = append(notes, fmt.Sprintf("低云量 %.0f%%（<3km，机位高度）成片遮挡", low))
		} else if low >= th.CloudSeaSuspectLowcloud {
			rating = model.Worse(rating, model.RATING_WARN)
			notes = append(notes, fmt.Sprintf("低云量 %.0f%% 偏高，头顶可能压云", low))
		}
	}
	// 中云（3–8km）：盖顶减光。
	if hasMid && mid >= th.MidCloudVeilCC {
		rating = model.Worse(rating, model.RATING_WARN)
		notes = append(notes, fmt.Sprintf("中云量 %.0f%%（3–8km）盖顶，星野受损", mid))
	}
	// 高云（8km+）：薄卷云减光。
	if hasHigh && high >= th.HighCloudThinVeilCC {
		rating = model.Worse(rating, model.RATING_WARN)
		notes = append(notes, fmt.Sprintf("高云量 %.0f%%（8km+ 卷云）减光", high))
	}
	// 能见度（雾/霾）。
	if hasVis {
		switch {
		case vis < th.FogVisibilityM:
			rating = model.Worse(rating, model.RATING_BAD)
			notes = append(notes, fmt.Sprintf("能见度 %.0fm，有雾/低云压顶", vis))
		case vis < th.HazeVisibilityM:
			rating = model.Worse(rating, model.RATING_WARN)
			notes = append(notes, fmt.Sprintf("能见度 %.0fm，轻雾/霾", vis))
		}
	}
	// 雾概率（clouds-1h 直供字段，比仅由能见度/湿度推断更可靠）。
	if hasFog {
		switch {
		case fogProb >= meteoFogProbBadPct:
			rating = model.Worse(rating, model.RATING_BAD)
			notes = append(notes, fmt.Sprintf("雾概率 %.0f%%，不宜拍摄", fogProb))
		case fogProb >= meteoFogProbWarnPct:
			rating = model.Worse(rating, model.RATING_WARN)
			notes = append(notes, fmt.Sprintf("雾概率 %.0f%%，警惕起雾", fogProb))
		}
	}

	// 结露 / LCL 提示：用温湿推算露点，不改评级（与 Open-Meteo 评估口径一致）。
	var spread float64
	hasSpread := false
	dew, hasDew := dewPointOf(temp, rh, hasTemp, hasRH)
	if hasTemp && hasDew {
		spread = temp - dew
		hasSpread = true
		if spread < th.DewSpreadC {
			notes = append(notes, fmt.Sprintf("温露差 %.1f℃，镜头结露风险", spread))
		}
		lcl := 124.0 * spread
		switch {
		case lcl < th.LCLAlertAGLM:
			notes = append(notes, fmt.Sprintf("LCL≈%.0fm，辐射雾风险高", lcl))
		case lcl < th.LCLWarnAGLM:
			notes = append(notes, fmt.Sprintf("LCL≈%.0fm，警惕起雾", lcl))
		}
	}

	// 天文量由本地算法计算，不依赖 Meteoblue 预报。
	// 偏移取自解析后时刻所属时区的实际偏移（含 DST；Shanghai 为恒定 +8h）。
	// 注意：绝不可用 localDT.Sub(localDT.UTC())——Sub 比较的是「同一时刻」，
	// 结果恒为 0，会把本地时刻误当 UTC，导致太阳/月亮高度算错 8 小时。
	_, utcOff := localDT.Zone()
	info := astro.Compute(localDT, utcOff, site.Lat, site.Lon, th.AstroDarkSunAlt)

	row := model.HourRow{
		Site:      site.Name,
		Lat:       site.Lat,
		Lon:       site.Lon,
		Alt:       site.Alt,
		TimeISO:   localDT.Format(timeLayout),
		TimeShort: localDT.Format("01-02 15:04"),
		Hour:      localDT.Hour(),
		Night:     night,
		HasData:   true,
		Time:      localDT,

		Rating:   rating,
		Note:     strings.Join(notes, "；"),
		Relation: model.Str(relationMeteoblueNoGeometry),

		CloudLow:       opt0(low, hasLow),
		CloudLowSource: model.Str("meteoblue"),
		CloudMid:       opt0(mid, hasMid),
		CloudHigh:      opt0(high, hasHigh),

		Visibility: opt1(vis, hasVis),
		Temp:       opt1(temp, hasTemp),
		Dew:        opt1(dew, hasDew),
		WindMS:     opt1(wind, hasWind),

		SunAlt:    model.Num(model.Round(info.SunAlt, 1)),
		MoonAlt:   model.Num(model.Round(info.MoonAlt, 1)),
		MoonIllum: model.Num(model.Round(info.MoonIllum*100.0, 0)),
		MoonPhase: info.MoonPhaseName,
		GCAlt:     model.Num(model.Round(info.GCAlt, 1)),
		AstroDark: info.AstroDark,
	}
	if hasSpread {
		row.Spread = opt1(spread, true)
		row.LCLAGLEst = opt0(124.0*spread, true)
	}
	return row
}

// fval 取第 i 个元素；缺测（越界或 null）返回 (0, false)。
func fval(a []*float64, i int) (float64, bool) {
	if i < 0 || i >= len(a) || a[i] == nil {
		return 0, false
	}
	return *a[i], true
}

// opt0/opt1 把 float64 装进 OptFloat，按 0/1 位小数取整；ok=false 时返回缺测。
func opt0(v float64, ok bool) model.OptFloat {
	if !ok {
		return model.Missing()
	}
	return model.Num(model.Round(v, 0))
}

func opt1(v float64, ok bool) model.OptFloat {
	if !ok {
		return model.Missing()
	}
	return model.Num(model.Round(v, 1))
}

// dewPointOf 由温度与相对湿度用 Magnus 公式推算露点；缺温或湿时返回缺测。
func dewPointOf(temp, rh float64, hasTemp, hasRH bool) (float64, bool) {
	if !hasTemp || !hasRH || rh <= 0 {
		return 0, false
	}
	a, b := 17.625, 243.04
	gamma := math.Log(rh/100.0) + a*temp/(b+temp)
	return b * gamma / (a - gamma), true
}

// inNightWindow 判断整点是否落在夜间窗口内（跨零点用「或」）。
// 与 core/analyse.go 的 InNightWindow 保持一致。
func inNightWindow(hour int, w config.WindowConfig) bool {
	return hour >= w.NightStartHour || hour <= w.NightEndHour
}

// nightIDOf 返回本地时刻所属观测夜 ID（YYYY-MM-DD），回拨 12h 归并次日凌晨。
// 与 core/analyse.go 的 NightIDOf 保持一致。
func nightIDOf(dt time.Time) string {
	return dt.Add(-12 * time.Hour).Format("2006-01-02")
}

// parseMetoTime 解析 Meteoblue 数据块的 time 字段。
// 本工具显式请求 timeformat=iso8601。实测 Meteoblue 返回的时刻**不带秒**
// （如 2026-08-12T22:00+08:00），与标准 RFC3339（含秒）不同，故需多个 layout
// 兜底；解析成功的时间已自带正确时区，天文量计算的 UTC 偏移直接取自该值。
func parseMetoTime(tStr string, loc *time.Location) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339,                  // 2006-01-02T15:04:05Z07:00（极少数构建带秒）
		"2006-01-02T15:04Z07:00",      // 2026-08-12T22:00+08:00（Meteoblue 实际返回）
		"2006-01-02T15:04:05",         // 裸本地时间（带秒）
		"2006-01-02T15:04",            // 裸本地时间（无秒）
	} {
		if t, err := time.Parse(layout, tStr); err == nil {
			return t, nil
		}
		if t, err := time.ParseInLocation(layout, tStr, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析 Meteoblue 时刻 %q", tStr)
}
