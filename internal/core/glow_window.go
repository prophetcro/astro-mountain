package core

import (
	"fmt"
	"math"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/astro"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
	"github.com/prophetcro/astro-mountain/internal/report"
)

// GlowWindow 是 report.GlowWindow 的别名。
//
// 结构体必须定义在 report 包：core 已经 import report，report 反向依赖 core 会成环。
// core 侧只负责计算与填充，report 侧负责渲染。
type GlowWindow = report.GlowWindow

const (
	// glowFadeElevDeg 是朝霞「消退」的太阳高度角（度）。
	// 太阳升到这么高后光线接近直射、色彩饱和度骤降，霞色发白消失。
	glowFadeElevDeg = 8.0
	// glowScanBeforeMin / glowScanAfterMin：扫描相对日出的前后范围（分钟）。
	glowScanBeforeMin = 90
	glowScanAfterMin  = 90
	// glowScanStepMin 是扫描太阳高度角的步长（分钟）。
	glowScanStepMin = 5
	// earthRadiusM 地球平均半径（米），地平俯角公式用。
	earthRadiusM = 6371008.0
	// glowCarrierCCMin 本地廓线里判定「可作朝霞载体」的最低层云量（%）。
	// 与朝霞档位的「中高云量 ≥5% 即小烧」、以及大气截面口径的
	// crossSectionCloudCoverMin 三者对齐，避免几何/档位两处门槛不一致。
	glowCarrierCCMin = 5.0
)

// horizonDipDeg 返回高度 hM（米，相对参照面）对应的地平俯角（度）：
//
//	dip(H) = acos(R / (R + H))，小 H 时近似 sqrt(2H/R) 弧度。
//
// hM 为负（云顶低于参照面）时按对称关系返回负值，于是「云顶先见光」的提前量
// 自动变成等量滞后——淹没型（云顶在机位上方）与脚下型（云顶在机位下方）
// 因此共用同一套物理，不必写两套公式。
func horizonDipDeg(hM float64) float64 {
	if math.Abs(hM) < 1e-6 {
		return 0
	}
	absH := math.Abs(hM)
	ratio := earthRadiusM / (earthRadiusM + absH)
	if ratio > 1 {
		ratio = 1
	}
	dip := math.Acos(ratio) * 180 / math.Pi
	if hM < 0 {
		return -dip
	}
	return dip
}

// sunAltAt 取某本地时刻的太阳地平高度角（度）。
//
// utcOffsetSec 由时刻自身所处时区推出：日出时刻来自 astro.SunriseTime，
// 挂在 FixedZone("local", utcOffsetSec) 上，故 sunrise.Zone() 就是真实偏移，
// 无需调用方额外传参。
func sunAltAt(t time.Time, lat, lon float64) float64 {
	_, offsetSec := t.Zone()
	return astro.Compute(t, offsetSec, lat, lon, -12).SunAlt
}

// ComputeGlowWindow 计算朝霞可持续窗口。
//
// 物理口径：
//   - 云顶比地面更早见到太阳，提前量 = 地平俯角 dip(H)，H = 云顶相对机位的高度（米）；
//   - 窗口起点 = 太阳高度角达到 −dip(H) 的时刻（云顶刚被晨光扫到）；
//   - 窗口终点 = 太阳高度角达到 glowFadeElevDeg 的时刻（太阳太高、霞色发白）；
//   - 在 [日出−90min, 日出+90min] 内按 5 分钟步长扫描太阳高度角，
//     并在跨过阈值的那一步内线性插值求精确时刻（5 分钟粒度对几十分钟的窗口太粗）。
//
// 无云载体（hasCloud=false）时返回空窗口（Lit=false、DurationMin=0），
// Reason 写明「无云载体」——朝霞判「无」就不该给用户一个假时间窗。
func ComputeGlowWindow(site model.Site, sunrise time.Time,
	cloudTopMSL float64, hasCloud bool) GlowWindow {

	if !hasCloud {
		return GlowWindow{Lit: false, DurationMin: 0, Reason: "无云载体"}
	}

	// 云顶相对机位的有效高度：淹没型为正（云顶在机位上方）、脚下型为负。
	// 两种形态共用同一套 dip 物理，符号决定窗口起点相对日出是提前还是滞后。
	h := cloudTopMSL - site.Alt
	// 云顶见光所需的太阳高度角：−dip(H)。H>0 时为负（早于日出），H<0 时为正（晚于日出）。
	startElev := -horizonDipDeg(h)
	if startElev >= glowFadeElevDeg {
		// 云顶过低（等效于机位下方数十公里）：见光门槛已高过消退角，窗口不成立。
		return GlowWindow{
			Lit:         false,
			DurationMin: 0,
			Reason: fmt.Sprintf("云顶相对机位 %.0fm，见光所需太阳高度 %.1f° 已高于消退角 %.0f°，无有效窗口",
				h, startElev, glowFadeElevDeg),
		}
	}

	scanStart := sunrise.Add(-time.Duration(glowScanBeforeMin) * time.Minute)
	scanEnd := sunrise.Add(time.Duration(glowScanAfterMin) * time.Minute)
	step := time.Duration(glowScanStepMin) * time.Minute

	winStart, okStart := elevationCrossing(scanStart, scanEnd, step, site, startElev, true)
	winEnd, okEnd := elevationCrossing(scanStart, scanEnd, step, site, glowFadeElevDeg, true)
	if !okStart {
		// 扫描范围内云顶始终未见光（高纬/异常时刻）：不给窗口，宁缺勿假。
		return GlowWindow{
			Lit:         false,
			DurationMin: 0,
			Reason: fmt.Sprintf("日出 ±%dmin 内太阳高度未达云顶见光所需的 %.1f°，无有效窗口",
				glowScanBeforeMin, startElev),
		}
	}
	if !okEnd {
		// 太阳一直没升到消退角（极地等）：窗口右端截断在扫描末端并如实说明。
		winEnd = scanEnd
	}

	dur := int(math.Round(winEnd.Sub(winStart).Minutes()))
	if dur < 0 {
		dur = 0
	}
	return GlowWindow{
		Start:       winStart,
		End:         winEnd,
		DurationMin: dur,
		Lit:         true,
		Reason: fmt.Sprintf("云顶 %.0fm（相对机位 %+.0fm，地平俯角 %.1f°），"+
			"云顶见光需太阳高度 %.1f°、%.0f° 时霞色消退",
			cloudTopMSL, h, horizonDipDeg(h), startElev, glowFadeElevDeg),
	}
}

// elevationCrossing 在 [from, to] 内按 step 扫描太阳高度角，求首次达到 target 度
// 的精确时刻（在跨阈值的那一步内线性插值）。rising=true 表示取上升穿越。
//
// 扫描固定 5 分钟步长，但返回值插值到分钟以下——5 分钟粒度对「转瞬即逝」的
// 朝霞窗口太粗，用户要的是几点几分到位。
func elevationCrossing(from, to time.Time, step time.Duration,
	site model.Site, target float64, rising bool) (time.Time, bool) {

	prevT := from
	prevAlt := sunAltAt(from, site.Lat, site.Lon)
	if rising && prevAlt >= target {
		return prevT, true
	}
	for t := from.Add(step); !t.After(to); t = t.Add(step) {
		alt := sunAltAt(t, site.Lat, site.Lon)
		if rising && prevAlt < target && alt >= target {
			return interpolateCrossing(prevT, prevAlt, t, alt, target), true
		}
		prevT, prevAlt = t, alt
	}
	return time.Time{}, false
}

// interpolateCrossing 在 [t0, t1] 内按太阳高度角线性插值出达到 target 的时刻。
// 相邻 5 分钟内太阳高度角近似线性，误差远小于步长本身。
func interpolateCrossing(t0 time.Time, a0 float64, t1 time.Time, a1 float64,
	target float64) time.Time {

	if a1 == a0 {
		return t1
	}
	frac := (target - a0) / (a1 - a0)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	return t0.Add(time.Duration(frac * float64(t1.Sub(t0))))
}

// glowCarrierTop 挑朝霞云载体的云顶海拔（MSL）与是否存在载体，供 ComputeGlowWindow 使用。
//
// 优先级：
//  1. 已跑出大气截面（--glow-cross-section）时，取被判定可照亮的云层高度——
//     取各采样点中最高的那个云层，它最先被晨光扫到；几何判定不可达则视为无载体。
//  2. 否则回落到本地廓线里最高的云层云顶（中/高云即朝霞载体）。
//  3. 朝霞档位判为「无」时直接视为无载体——没有可染红的云，就不给用户一个假时间窗。
func glowCarrierTop(site Site, resp *api.Response, sunrise time.Time, cfg config.Config,
	glow DawnGlowContext, tier string) (topMSL float64, hasCloud bool) {

	if tier == "无" {
		return 0, false
	}
	if len(glow.CrossSection) > 0 {
		lit, _, _ := AssessCrossSection(glow.CrossSection)
		if !lit {
			return 0, false
		}
		best := math.Inf(-1)
		for _, p := range glow.CrossSection {
			if p.HasCloud && p.CloudBaseMSL > best {
				best = p.CloudBaseMSL
			}
		}
		if math.IsInf(best, -1) {
			return 0, false
		}
		return best, true
	}
	top, ok := localCloudTopMSL(site, resp, sunrise, cfg)
	if !ok {
		return 0, false
	}
	return top, true
}

// localCloudTopMSL 取日出时次本地廓线里的朝霞云载体云顶海拔（MSL）。
//
// 中高云是朝霞的载体，故取「机位以上最高的云顶」——它最早被晨光扫到。
// 两级取值：
//  1. 优先用 DetectLayers 反演出的成层云顶（层边界做过插值，更精确）；
//  2. DetectLayers 切不出连续云层时，退回逐层云量找机位以上最高的有云层。
//
// 第 2 级不能省：朝霞档位本来就是按中/高云量给的，若此处一律判「无载体」，
// 就会出现报告写「小烧」却又写「无可染红云载体」的自相矛盾。
func localCloudTopMSL(site Site, resp *api.Response, sunrise time.Time,
	cfg config.Config) (float64, bool) {

	if resp == nil || len(resp.Times) == 0 {
		return 0, false
	}
	sunriseIdx := glowCloudIndex(resp, sunrise)
	if top, ok := cloudTopAtIndex(resp, cfg, sunriseIdx, site.Alt); ok {
		return top, true
	}
	// 日出时次的廓线没有云层时，沿全夜扫描取最高云顶。
	// 必要性：朝霞档位自己就有「缺测/为零时回退全夜最大中高云量」的兜底，
	// 若云顶高度只看日出那一个时次，就会出现档位取自 A 时次、云顶取自 B 时次的分叉，
	// 报告上表现为「判大烧却说无云载体」的自相矛盾。
	best, found := 0.0, false
	for idx := range resp.Times {
		if idx == sunriseIdx {
			continue
		}
		top, ok := cloudTopAtIndex(resp, cfg, idx, site.Alt)
		if ok && (!found || top > best) {
			best, found = top, true
		}
	}
	return best, found
}

// cloudTopAtIndex 取第 idx 个时次的云载体云顶（MSL）：
// 优先 DetectLayers 的成层云顶，否则用逐层云量找机位以上最高的有云层。
func cloudTopAtIndex(resp *api.Response, cfg config.Config, idx int, siteAlt float64) (float64, bool) {
	levels := profile.BuildProfile(resp.LevelValues(idx), cfg.Thresh)
	if top, ok := highestLayerTop(levels, cfg); ok {
		return top, true
	}
	return highestCloudyLevelHeight(levels, siteAlt, glowCarrierCCMin)
}

// highestLayerTop 取 DetectLayers 反演出的最高云层云顶（MSL）。
func highestLayerTop(levels []profile.Level, cfg config.Config) (float64, bool) {
	layers := profile.DetectLayers(levels, cfg.Thresh)
	best, found := 0.0, false
	for _, l := range layers {
		if !found || l.TopMSL > best {
			best, found = l.TopMSL, true
		}
	}
	return best, found
}

// highestCloudyLevelHeight 在逐层廓线里找云量 ≥ minCC 的最高一层高度（MSL）。
// 优先取机位以上的层（中高云才是朝霞载体）；机位以上一层都没有时，
// 退回廓线里最高的有云层——此时云顶低于机位，窗口起点相应后移（由 dip 的符号处理）。
func highestCloudyLevelHeight(levels []profile.Level, siteAlt, minCC float64) (float64, bool) {
	best, found := 0.0, false
	for _, l := range levels {
		if l.CCV() < minCC || l.Height <= siteAlt {
			continue
		}
		if !found || l.Height > best {
			best, found = l.Height, true
		}
	}
	if found {
		return best, true
	}
	for _, l := range levels {
		if l.CCV() < minCC {
			continue
		}
		if !found || l.Height > best {
			best, found = l.Height, true
		}
	}
	return best, found
}

// glowCloudIndex 找时间轴上离日出最近的时次下标。
//
// 与 dawnGlowCloud 同一口径：日出是 FixedZone 墙钟、响应轴是 UTC 承载墙钟，
// 必须先剥时区再比，否则会整整错开一个时区偏移（8 小时）命中昨夜的时次。
func glowCloudIndex(resp *api.Response, sunrise time.Time) int {
	sunriseWall := wallClockUTC(sunrise)
	bestIdx, bestDelta := 0, float64(1<<62)
	for idx, t := range resp.Times {
		d := math.Abs(wallClockUTC(t).Sub(sunriseWall).Minutes())
		if d < bestDelta {
			bestDelta, bestIdx = d, idx
		}
	}
	return bestIdx
}
