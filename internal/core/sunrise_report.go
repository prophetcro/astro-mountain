package core

import (
	"fmt"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/astro"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
	"github.com/prophetcro/astro-mountain/internal/report"
)

// BuildSunriseReport 为单站点计算「日出云海模式」的聚合结果。
//
// 输入：
//   - site / resp：点位与已抓取的原始预报响应（A 轨）。
//   - targetNight：观测夜 ID（日出当天的「前一夜」）。
//   - sunriseDate：用户选定的日出当天（本地日历日）。
//   - cfg：已在调用方把 NightEndHour 放宽到包含日出时分的配置副本。
//   - utcOffsetSec：API 响应自带的 UTC 偏移秒数（用于算日出时刻）。
//   - arriveBufferMin：配置里的 arrive_buffer_min（相对日出的提前缓冲）。
//
// 输出 report.SunriseSiteResult（云海时段 / 云海形态 / 朝霞四档 / 建议抵达时间 / 云海可信度五档）。
// 注意：类型定义在 report 包，避免 report 反向依赖 core 形成循环引用。
func BuildSunriseReport(site Site, resp *api.Response, targetNight string,
	sunriseDate time.Time, cfg config.Config, utcOffsetSec int, arriveBufferMin int) report.SunriseSiteResult {

	res := report.SunriseSiteResult{Site: site.Name}

	sunrise, ok := astro.SunriseTime(site.Lat, site.Lon, utcOffsetSec, sunriseDate)
	if !ok {
		// 极地等异常：退回 06:30 占位，避免下游空值导致整段结论崩塌。
		loc := time.FixedZone("local", utcOffsetSec)
		sunrise = time.Date(sunriseDate.Year(), sunriseDate.Month(), sunriseDate.Day(), 6, 30, 0, 0, loc)
	}
	res.SunriseTime = sunrise
	// 建议抵达机位时间 = 日出 − (缓冲 + 上山车程)；车程 0 时即纯缓冲。
	res.ArriveBy = sunrise.Add(-time.Duration(arriveBufferMin+site.DriveMinutes) * time.Minute)

	// 云海时段：复用 Phase 1 的 CollectCloudSeaEpisodesForNight（独立重算，不改 HourRow）。
	eps := CollectCloudSeaEpisodesForNight(site, resp, targetNight, cfg)
	res.Episodes = eps
	for _, e := range eps {
		res.CloudSeaHours += e.HoursCount
	}
	res.HasData = len(resp.Times) > 0
	res.CloudSeaForm = cloudSeaFormOf(eps)

	// 朝霞：取离日出时刻最近的整点云量评估（±40min 内），缺测则兜底全夜最大中高云量。
	glowLow, glowMid, glowHigh := dawnGlowCloud(resp, sunrise)
	res.DawnGlow, res.DawnGlowNote = assessDawnGlow(glowLow, glowMid, glowHigh)

	// 可信度：云海时次 + 时段数 + 模式垂直分辨率（机位上下相邻层间距）。
	// 只要有一段是「淹没型」（机位埋在云层顶部附近），可信度封顶「中」——
	// 人就在云里，能见度与稳定性都差，给「高/极高」是伪精度、会让人白跑。
	submerged := false
	for _, e := range eps {
		if e.Submerged {
			submerged = true
			break
		}
	}
	vgap := nightVerticalGap(site, resp, targetNight, cfg)
	res.Confidence, res.ConfidenceNote = assessSunriseConfidence(
		res.CloudSeaHours, len(eps), vgap, submerged)

	res.Rating = sunriseVerdict(res)
	return res
}

// dawnGlowCloud 在日出时刻附近找一行，返回其低/中/高云量（缺测回退到全夜最大中高云量）。
func dawnGlowCloud(resp *api.Response, sunrise time.Time) (low, mid, high float64) {
	bestIdx := -1
	var bestDelta int64 = 1 << 62
	for idx, localDT := range resp.Times {
		d := absMinutes(localDT.Sub(sunrise))
		if d < bestDelta {
			bestDelta = d
			bestIdx = idx
		}
	}
	if bestIdx < 0 {
		return 0, 0, 0
	}
	s := resp.Surface(bestIdx)
	low = optOrF(s.CloudCoverLow, 0)
	mid = optOrF(s.CloudCoverMid, 0)
	high = optOrF(s.CloudCoverHigh, 0)
	// 日出附近缺测时，用全夜最大中高云量兜底，避免漏判「有云可烧」。
	if mid == 0 && high == 0 {
		for idx := range resp.Times {
			ss := resp.Surface(idx)
			if ss.CloudCoverMid.Valid && ss.CloudCoverMid.V > mid {
				mid = ss.CloudCoverMid.V
			}
			if ss.CloudCoverHigh.Valid && ss.CloudCoverHigh.V > high {
				high = ss.CloudCoverHigh.V
			}
		}
	}
	return low, mid, high
}

func optOrF(v model.OptFloat, def float64) float64 {
	if v.Valid {
		return v.V
	}
	return def
}

func absMinutes(d time.Duration) int64 {
	m := d.Milliseconds() / 60000
	if m < 0 {
		m = -m
	}
	return m
}

// assessDawnGlow 据中高云量与低云遮挡判朝霞四档。
// 朝霞需要：中高云（被日出染红）且低空通透（无厚云压顶遮挡日出处）。
func assessDawnGlow(low, mid, high float64) (string, string) {
	if low >= 60 {
		return "无", fmt.Sprintf("低云量 %.0f%% 偏高，日出处被遮挡，朝霞难现", low)
	}
	midhigh := mid
	if high > midhigh {
		midhigh = high
	}
	switch {
	case midhigh >= 40:
		return "大烧", fmt.Sprintf("中高云量 %.0f%% 适中，日出处有云可染红，朝霞条件佳", midhigh)
	case midhigh >= 20:
		return "中烧", fmt.Sprintf("中高云量 %.0f%%，朝霞中等", midhigh)
	case midhigh >= 5:
		return "小烧", fmt.Sprintf("仅薄高云 %.0f%%，朝霞微弱", midhigh)
	default:
		return "无", "无中高云载体，晴天无朝霞（或全低云压顶）"
	}
}

// nightVerticalGap 取该夜首个可用廓线的机位上下相邻层间距，反映模式垂直分辨率。
func nightVerticalGap(site Site, resp *api.Response, targetNight string, cfg config.Config) float64 {
	for idx, localDT := range resp.Times {
		if NightIDOf(localDT) != targetNight {
			continue
		}
		levels := profile.BuildProfile(resp.LevelValues(idx), cfg.Thresh)
		if ProfileUsable(levels) {
			return profile.MaxGapAroundSite(levels, site.Alt)
		}
	}
	return 0
}

// cloudSeaFormOf 据云海时段归纳整站点的云海形态，便于报告与汇总表直接展示。
//
// 判定：没有任何云海时段 → 空串（渲染层据此跳过）；
// 若同时存在「脚下型」与「淹没型」→ 「脚下型+淹没型」（混合形态，如实标注）；
// 仅淹没型 → 「淹没型」；否则（全为脚下型）→ 「脚下型」。
// Submerged 字段由 ClassifySeaGeometry 统一口径给出，是形态的唯一权威来源，
// 不在此处重新实现几何判定（重演 P0 漏检教训）。
func cloudSeaFormOf(eps []report.CloudSeaEpisode) string {
	if len(eps) == 0 {
		return ""
	}
	submerged, below := 0, 0
	for _, e := range eps {
		if e.Submerged {
			submerged++
		} else {
			below++
		}
	}
	switch {
	case submerged > 0 && below > 0:
		return "脚下型+淹没型"
	case submerged > 0:
		return "淹没型"
	default:
		return "脚下型"
	}
}

// assessSunriseConfidence 给出云海出现的诚实五档可信度（绝不伪造百分比）。
// 五档：极高 / 高 / 中 / 低 / 极低。依据：云海持续时次、云海段数、模式垂直分辨率。
// 缺云海即「极低」；垂直分辨率不足（机位上下层间距 >500m）降为「低」；
// 只要有一段是淹没型（机位埋在云中）封顶「中」。
//
// 三道压制的优先级：分辨率不足 > 淹没型 > 时长分档。
// 分辨率不足最致命（几何反演本身就不可靠），其次是人就在云里拍不到。
func assessSunriseConfidence(cloudSeaHours, episodes int, vgap float64,
	submerged bool) (string, string) {

	if cloudSeaHours == 0 {
		return "极低", "预报窗口内未检出云海（机位下方无连续云面）"
	}
	// 模式垂直分辨率不足（机位上下相邻层间距 >500m）时，无论云海多长都只能给「低」：
	// 反演出的云底/云顶几何不可靠，继续给高可信度是伪精度。
	badRes := vgap > 500
	if badRes {
		return "低", fmt.Sprintf("云海检出 %d 时次，但模式垂直分辨率不足（机位上下层间距 %.0fm），"+
			"反演的云海几何不可靠，判定置信度有限", cloudSeaHours, vgap)
	}
	// 淹没型封顶「中」：机位本身埋在云层顶部附近，脚下虽有云海，
	// 但人在云中、能见度差，只能守候云隙破云，稳定性远不如脚下型。
	if submerged {
		return "中", fmt.Sprintf("云海检出 %d 时次、%d 段，但机位被云顶淹没"+
			"（人处在云中，可守候云隙破云，能见度与稳定性都差），可信度封顶「中」",
			cloudSeaHours, episodes)
	}
	switch {
	case cloudSeaHours >= 8 && episodes >= 1:
		return "极高", fmt.Sprintf("云海持续 %d 时次、%d 段，模式垂直分辨率充足，可放心守候", cloudSeaHours, episodes)
	case cloudSeaHours >= 6 && episodes >= 1:
		return "高", fmt.Sprintf("云海持续 %d 时次、%d 段，模式垂直分辨率充足", cloudSeaHours, episodes)
	case cloudSeaHours >= 3 && episodes >= 1:
		return "中", fmt.Sprintf("云海检出 %d 时次、%d 段，可守候", cloudSeaHours, episodes)
	default:
		return "中", fmt.Sprintf("云海检出 %d 时次，但时段偏短", cloudSeaHours)
	}
}

// sunriseVerdict 把聚合结果压成一句话结论。
func sunriseVerdict(r report.SunriseSiteResult) string {
	if !r.HasData {
		return "❓ 无有效数据"
	}
	if r.CloudSeaHours == 0 {
		if r.DawnGlow != "无" {
			return "☀️ 无云海，但朝霞可拍（" + r.DawnGlow + "）"
		}
		return "🔴 该夜无云海、朝霞亦弱"
	}
	switch r.Confidence {
	case "极高", "高":
		return "✅ 云海大概率可拍 + 朝霞" + r.DawnGlow
	case "中":
		return "⚠️ 云海有机会（" + r.DawnGlow + "），需现场守候"
	default:
		return "⚠️ 云海存疑（" + r.DawnGlow + "），谨慎前往"
	}
}
