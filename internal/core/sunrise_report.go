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
	sunriseDate time.Time, cfg config.Config, utcOffsetSec int, arriveBufferMin int,
	glow DawnGlowContext) report.SunriseSiteResult {

	res := report.SunriseSiteResult{Site: site.Name, SunriseDate: sunriseDate.Format(DateLayout)}

	sunrise, ok := astro.SunriseTime(site.Lat, site.Lon, utcOffsetSec, sunriseDate)
	if !ok {
		// 极地等异常：退回 06:30 占位，避免下游空值导致整段结论崩塌。
		loc := time.FixedZone("local", utcOffsetSec)
		sunrise = time.Date(sunriseDate.Year(), sunriseDate.Month(), sunriseDate.Day(), 6, 30, 0, 0, loc)
	}
	res.SunriseTime = sunrise
	// 建议抵达机位时间 = 日出 − 缓冲（arrive_buffer_min）。
	res.ArriveBy = sunrise.Add(-time.Duration(arriveBufferMin) * time.Minute)

	// 云海时段：复用 Phase 1 的 CollectCloudSeaEpisodesForNight（独立重算，不改 HourRow）。
	eps := CollectCloudSeaEpisodesForNight(site, resp, targetNight, cfg)
	res.Episodes = eps
	for _, e := range eps {
		res.CloudSeaHours += e.HoursCount
	}
	res.HasData = len(resp.Times) > 0
	res.CloudSeaForm = cloudSeaFormOf(eps)

	// 朝霞：取离日出时刻最近的整点云量评估（±40min 内），缺测则兜底全夜最大中高云量。
	// 主模式先按云量给档位，再用 AOD 降级，最后与多模型共识对齐（抓单模型离群）。
	glowLow, glowMid, glowHigh := dawnGlowCloud(resp, sunrise)
	primTier, primNote := assessDawnGlow(glowLow, glowMid, glowHigh)
	primTier, primNote = degradeDawnGlowByAOD(primTier, primNote, glow.AOD)
	finalTier, finalNote, _ := consensusDawnGlow(primTier, primNote, glow.Compare)
	res.DawnGlow, res.DawnGlowNote = finalTier, finalNote

	// 近地体积雾：日出拍摄窗口内逐时判定，取最强的一档。
	// 这是独立于云海判定的正面信号——近地雾是贴地现象，与「脚下有没有云海」
	// 由两套完全不同的判据给出（云海看气压层廓线几何，近地雾看地面要素），
	// 两者互不覆盖，也不参与云海可信度与朝霞档位的计算。
	fog := assessDawnGroundFog(resp, sunrise, cfg)
	res.FogPotential, res.FogNote = fog.Level, fog.Note

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

// wallClockUTC 把任意时区的时刻剥去时区、只保留墙钟（年月日时分秒），统一用 UTC 承载。
//
// 为什么必须剥时区：astro.SunriseTime 返回的是 FixedZone("local", utcOffsetSec) 下的
// **当地墙钟**（如 06:03 +0800，其绝对瞬间是前一日 22:03Z）；
// 而 api.Response.Times 是「把 UTC 偏移加进去之后用 UTC 承载的当地墙钟」（06:03 记作 06:03Z）。
// 两者直接相减会比真实墙钟差整整一个时区偏移（本项目 UTC+8，即 +8h），
// 于是「距日出 3 分钟的 06:00」会被算成「距日出 8 小时」，
// 而昨夜 23:00 反倒成了「最近时次」——朝霞取云量、近地雾取窗口都会因此取错小时。
// 与 report.sunriseWindowContains 同一口径：比较前先剥时区，只比墙钟。
func wallClockUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// dawnGlowCloud 在日出时刻附近找一行，返回其低/中/高云量（缺测回退到全夜最大中高云量）。
//
// 「附近」按**墙钟**比较（见 wallClockUTC）：不剥时区时，日出后 3 分钟的时次会被算成
// 8 小时之外，选中的反而是前一夜 23:00，朝霞档位因此用了完全不相干的云量。
func dawnGlowCloud(resp *api.Response, sunrise time.Time) (low, mid, high float64) {
	sunriseWall := wallClockUTC(sunrise)
	bestIdx := -1
	var bestDelta int64 = 1 << 62
	for idx, localDT := range resp.Times {
		d := absMinutes(wallClockUTC(localDT).Sub(sunriseWall))
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

// assessDawnGlow 据中高云量与低云遮挡判朝霞四档（纯云量口径，不含 AOD）。
// 朝霞需要：中高云（被日出染红）且低空通透（无厚云压顶遮挡日出处）。
//
// 结构护栏：中高云接近满云（>=90%）通常是不可染的厚云层而非可染薄云，封顶中烧——
// 它改变了原「midhigh>=40 即大烧」的盲目乐观，但并不触碰「aod 缺失且 midhigh<90」
// 的回归区间（该区间输出与原版逐字节一致，见 TestAssessDawnGlow）。
func assessDawnGlow(low, mid, high float64) (string, string) {
	if low >= 60 {
		return "无", fmt.Sprintf("低云量 %.0f%% 偏高，日出处被遮挡，朝霞难现", low)
	}
	midhigh := mid
	if high > midhigh {
		midhigh = high
	}
	if midhigh < 5 {
		return "无", "无中高云载体，晴天无朝霞（或全低云压顶）"
	}
	var tier, note string
	switch {
	case midhigh >= 40:
		tier, note = "大烧", fmt.Sprintf("中高云量 %.0f%% 适中，日出处有云可染红，朝霞条件佳", midhigh)
	case midhigh >= 20:
		tier, note = "中烧", fmt.Sprintf("中高云量 %.0f%%，朝霞中等", midhigh)
	default: // >=5
		tier, note = "小烧", fmt.Sprintf("仅薄高云 %.0f%%，朝霞微弱", midhigh)
	}
	// 结构护栏：满云封顶中烧。
	if midhigh >= 90 && tier == "大烧" {
		tier = "中烧"
		note = fmt.Sprintf("中高云量 %.0f%% 过饱和（厚云层），朝霞封顶中烧", midhigh)
	}
	return tier, note
}

// glowCompareModels 是朝霞多模型共识用于交叉验证的辅助模式（不含主模式）。
// 与 mcpdiag glow-data 的默认四模型口径一致，覆盖 GFS/ECMWF/最佳融合，
// 专门用来抓「主模式（默认 ICON）系统性高估中层云」这类离群。
var glowCompareModels = []string{"gfs_seamless", "ecmwf_ifs025", "best_match"}

// DawnGlowContext 携带朝霞评估所需的辅助数据：AOD 与多模型共识输入。
// 单模型运行（无对比 / --no-cross-model）下可留空，退化为原单模型口径（回归安全）。
type DawnGlowContext struct {
	// AOD 是日出时刻的 CAMS 气溶胶光学厚度；Invalid 表示未取到（不降级）。
	AOD model.OptFloat
	// Compare 是辅助模式名 -> 该模式算出的朝霞档位（已含 AOD 降级）；不含主模式。
	Compare map[string]string
}

// dawnGlowTierRank 朝霞档位排序：无<小烧<中烧<大烧。
func dawnGlowTierRank(tier string) int {
	switch tier {
	case "小烧":
		return 1
	case "中烧":
		return 2
	case "大烧":
		return 3
	}
	return 0 // 无 / 未知
}

func dawnGlowTierName(rank int) string {
	switch rank {
	case 1:
		return "小烧"
	case 2:
		return "中烧"
	case 3:
		return "大烧"
	}
	return "无"
}

// degradeDawnGlowByAOD 据气溶胶光学厚度对朝霞档位做降级。
// AOD 过高时天空发灰、散射吞掉鲜艳度，不应报大烧。AOD 缺失则不降级（沿用原档位）。
func degradeDawnGlowByAOD(tier, note string, aod model.OptFloat) (string, string) {
	if !aod.Valid {
		return tier, note
	}
	switch {
	case aod.V >= 1.0:
		return "无", fmt.Sprintf("气溶胶 AOD %.2f 过高，天空浑浊，朝霞难现", aod.V)
	case aod.V >= 0.6:
		if dawnGlowTierRank(tier) > 1 {
			return "小烧", fmt.Sprintf("中高云可用，但 AOD %.2f 偏高、天空发灰，朝霞封顶小烧", aod.V)
		}
	case aod.V >= 0.3:
		if dawnGlowTierRank(tier) > 2 {
			return "中烧", fmt.Sprintf("中高云可用，但 AOD %.2f 偏高，朝霞封顶中烧", aod.V)
		}
	}
	return tier, note
}

// consensusDawnGlow 多模型共识：当多数模式与单一模式结论冲突时，以多数模式为准（封顶），
// 避免单个模式的系统性离群（如 ICON 对中层云的高估）把「不烧」误报成「大烧」。
//
// compare 不含主模式；为空表示单模型，直接返回主模式结论（回归安全）。
// 本函数只降不升：若多数模式比主模式更高（主模式偏保守），保留主模式结论，
// 以「不谎报大烧」为第一优先级。返回 final 档位、note、是否发生分歧降级。
func consensusDawnGlow(primaryTier, primaryNote string, compare map[string]string) (final, note string, divergent bool) {
	if len(compare) == 0 {
		return primaryTier, primaryNote, false
	}
	all := make([]int, 0, len(compare)+1)
	all = append(all, dawnGlowTierRank(primaryTier))
	for _, t := range compare {
		all = append(all, dawnGlowTierRank(t))
	}
	n := len(all)
	majority := n/2 + 1

	// 共识档位 = 满足「≥该档位的模式数不少于多数」的最高档位。
	consensusRank := 0
	for rank := 3; rank >= 0; rank-- {
		cnt := 0
		for _, r := range all {
			if r >= rank {
				cnt++
			}
		}
		if cnt >= majority {
			consensusRank = rank
			break
		}
	}

	primRank := dawnGlowTierRank(primaryTier)
	if primRank <= consensusRank {
		// 主模式未高于共识：无需封顶（本函数只降不升，保持诚实保守）。
		return primaryTier, primaryNote, false
	}

	// 主模式高于共识：封顶到共识档位，并标注分歧。
	final = dawnGlowTierName(consensusRank)
	belowBig := 0
	for _, r := range all {
		if r < 3 {
			belowBig++
		}
	}
	note = fmt.Sprintf("%s；⚠️低可信度（模型分歧）：%d/%d 模式未达大烧，已按多数模型封顶为%s",
		primaryNote, belowBig, n, final)
	return final, note, true
}

// assessDawnGroundFog 聚合「日出拍摄窗口」内的近地体积雾档位，取最强的一档。
//
// 窗口 = [日出 − SunriseWindowBeforeMin, 日出 + SunriseWindowAfterMin]（默认 −45~+30min），
// 与朝霞取值的口径一致：用户真正在现场按快门的就是这一段时间。
// 雾是逐时演变的（日出前最重、日出后抬升消散），故取窗口内**最强**档而非平均——
// 平均会把「前半小时有雾、后半小时散了」稀释成「无雾」，那正是用户想避免的漏报。
//
// 窗口内没有任何时次时（模式分辨率粗于窗口宽度，如 3h 数据），
// 退回离日出最近的那个时次，并在 Note 里注明，不假装窗口内真有数据。
// 时间轴为空则返回「无」+ 说明，绝不编造。
//
// 判定本身一律由 profile.AssessGroundFog 给出（能见度权威、缺测降级到近地 RH 代理），
// 此处只负责挑时次，不复制任何判据——避免与逐小时评级出现口径分叉。
func assessDawnGroundFog(resp *api.Response, sunrise time.Time, cfg config.Config) profile.FogAssessment {
	if resp == nil || len(resp.Times) == 0 {
		return profile.FogAssessment{
			Level: profile.FOG_NONE,
			Note:  "预报时间轴为空，无法判定近地雾",
		}
	}

	before := cfg.Window.SunriseWindowBeforeMin
	if before < 0 {
		before = 0
	}
	after := cfg.Window.SunriseWindowAfterMin
	if after < 0 {
		after = 0
	}
	winBefore := time.Duration(before) * time.Minute
	winAfter := time.Duration(after) * time.Minute

	// 日出时刻与响应时间轴的时区承载方式不同（前者 FixedZone 墙钟、后者 UTC 承载墙钟），
	// 比较前统一剥时区，否则整个窗口会整体平移一个时区偏移、命中昨夜的时次。
	sunriseWall := wallClockUTC(sunrise)

	best := profile.FogAssessment{Level: profile.FOG_NONE}
	bestRank := -1
	bestAbs := int64(1 << 62)
	found := false

	for idx, localDT := range resp.Times {
		// delta > 0 表示该时次晚于日出；窗口为 [−before, +after]。
		delta := wallClockUTC(localDT).Sub(sunriseWall)
		if delta > winAfter || delta < -winBefore {
			continue
		}
		a := profile.AssessGroundFog(resp.Surface(idx), cfg.Thresh)
		absM := absMinutes(delta)
		// 同档位时取更靠近日出的那一时次：它离拍摄时刻最近，也最可信。
		if r := profile.FogLevelRank(a.Level); r > bestRank || (r == bestRank && absM < bestAbs) {
			best, bestRank, bestAbs = a, r, absM
		}
		found = true
	}

	if found {
		return best
	}

	// 窗口内没有时次：退回最近时次并如实标注。
	nearestIdx, nearestAbs := -1, int64(1<<62)
	for idx, localDT := range resp.Times {
		if d := absMinutes(wallClockUTC(localDT).Sub(sunriseWall)); d < nearestAbs {
			nearestAbs, nearestIdx = d, idx
		}
	}
	if nearestIdx < 0 {
		return profile.FogAssessment{Level: profile.FOG_NONE, Note: "预报时间轴为空，无法判定近地雾"}
	}
	fallback := profile.AssessGroundFog(resp.Surface(nearestIdx), cfg.Thresh)
	if fallback.Note != "" {
		fallback.Note += "；"
	}
	fallback.Note += fmt.Sprintf("日出拍摄窗口（−%d~+%dmin）内无模式时次，改用距日出 %d 分钟的最近时次",
		before, after, nearestAbs)
	return fallback
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
