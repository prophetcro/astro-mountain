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

	// 淹没型：机位是否被云顶淹没（云顶高于机位）。同一标志同时用于
	// ① 云海可信度封顶「中」（人在云里能见度差）；
	// ② 朝霞物理地板（淹没型→云顶必被晨光染红，见 applySubmergedGlowFloor）。
	// 提前到此处计算，避免与下游重复循环。
	submerged := false
	for _, e := range eps {
		if e.Submerged {
			submerged = true
			break
		}
	}

	// 朝霞：取离日出时刻最近的整点云量评估（±40min 内），缺测则兜底全夜最大中高云量。
	// 主模式先按云量给档位，再用 AOD 降级，最后与多模型共识对齐（抓单模型离群）。
	glowLow, glowMid, glowHigh := dawnGlowCloud(resp, sunrise)
	primTier, primNote := assessDawnGlow(glowLow, glowMid, glowHigh)
	primTier, primNote = degradeDawnGlowByAOD(primTier, primNote, glow.AOD)

	// 朝霞最终档位：
	//   - 默认 "" / "consensus"：多模型共识封顶（只降不升，抓 ICON 等单模型离群）；
	//   - "loose"：主模型(ICON)与 ECMWF 取最高档（任一报大烧即采纳）；
	//   - "sunset"：sunsetbot 口径——以 GFS/ECMWF 两个基准模型取低（任一方判无即无），
	//     彻底排除 ICON 系统性高估中层云的离群（不抬高也不压低主模型）。
	var finalTier, finalNote string
	switch glow.GlowPolicy {
	case "loose":
		finalTier, finalNote = looseDawnGlow(primTier, glow.PrimaryModel, glow.Compare)
	case "sunset":
		// 评估时次太阳地平高度：几何门用它判断云层是否已被照亮（见 sunsetDawnGlow）。
		sunElev := astro.Compute(sunrise, utcOffsetSec, site.Lat, site.Lon, -12).SunAlt
		finalTier, finalNote = sunsetDawnGlow(primTier, glow.Models, glow.AOD, sunElev)
	default: // "consensus" 或空
		finalTier, finalNote, _ = consensusDawnGlow(primTier, primNote, glow.Compare)
	}
	res.DawnGlow, res.DawnGlowNote = finalTier, finalNote

	// 大气截面几何判定（对齐 sunsetbot 的 800km 截面 + AOD 等效云底）：
	// 若沿太阳方向没有任何云层可被曙/暮光照射，则几何上无辉光，封顶「无」
	// 并附几何原因；若可达，则把可达点数补进原因，便于透明化。
	if len(glow.CrossSection) > 0 {
		lit, _, csNote := AssessCrossSection(glow.CrossSection)
		if !lit {
			if finalTier != "无" {
				finalTier = "无"
			}
			finalNote = csNote
		} else {
			finalNote = finalNote + "；" + csNote
		}
		res.DawnGlow, res.DawnGlowNote = finalTier, finalNote
	}

	// 淹没型朝霞物理修正：机位埋于云顶附近 → 云顶必被晨光染红，朝霞地板抬到中烧。
	// 放在几何/截面门之后，覆盖它们对「贴身云海」的低估（那些门沿太阳方向采样远处云层，抓不到脚下这层）。
	res.DawnGlow, res.DawnGlowNote = applySubmergedGlowFloor(res.DawnGlow, res.DawnGlowNote, submerged, glow.AOD)

	// 朝霞窗口：把「能烧多久、几点到几点该守」补上（放在所有朝霞封顶之后，
	// 保证用的是最终档位——档位被几何门封成「无」时窗口必须同步留空）。
	// 只给档位不给时间，用户不知道该几点到位——实测反馈「朝霞转瞬即逝」正是这个缺口。
	// 云载体高度优先取大气截面里可照亮的云层，否则回落本地廓线最高云层云顶。
	glowTop, hasGlowCarrier := glowCarrierTop(site, resp, sunrise, cfg, glow, res.DawnGlow)
	res.GlowWindow = ComputeGlowWindow(site, sunrise, glowTop, hasGlowCarrier)

	// 分歧透明化：把每个模型的原始档位都摊开，决策权交给用户。
	// 优先用全模型明细 glow.Models（sunset 口径填充了全部 4 个模型）；
	// 缺失时回落到「主模型 + 对比模型」（consensus/loose 旧口径），保证回归安全。
	if len(glow.Models) > 0 {
		bd := orderedGlowModels(glow.Models, glow)
		res.DawnGlowModels = bd
		res.DawnGlowDivergence = dawnGlowDivergenceLabelFromMap(glow.Models)
	} else if glow.PrimaryModel != "" || len(glow.Compare) > 0 {
		bd := make([]report.DawnGlowModelVerdict, 0, len(glow.Compare)+1)
		if glow.PrimaryModel != "" {
			bd = append(bd, report.DawnGlowModelVerdict{
				Model: glow.PrimaryModel, Tier: primTier, Primary: true,
			})
		}
		for m, t := range glow.Compare {
			bd = append(bd, report.DawnGlowModelVerdict{Model: m, Tier: t})
		}
		res.DawnGlowModels = bd
		res.DawnGlowDivergence = dawnGlowDivergenceLabel(primTier, glow.Compare)
	}

	// 近地体积雾：日出拍摄窗口内逐时判定，取最强的一档。
	// 这是独立于云海判定的正面信号——近地雾是贴地现象，与「脚下有没有云海」
	// 由两套完全不同的判据给出（云海看气压层廓线几何，近地雾看地面要素），
	// 两者互不覆盖，也不参与云海可信度与朝霞档位的计算。
	fog := assessDawnGroundFog(resp, sunrise, cfg)
	res.FogPotential, res.FogNote = fog.Level, fog.Note
	// 辐射雾时段：把日出夜间逐时雾档≥中(可拍)的连续小时聚合成时段，
	// 独立于「云海时段」渲染——避免把贴地辐射雾误读成脚下云海（抖音常混称「云瀑」）。
	res.FogPeriods = assessDawnGroundFogPeriods(resp, sunrise, cfg)

	// 日出窗被云雾覆盖警告：辐射雾或淹没型云海完整盖住日出拍摄窗口 →
	// 机位处被云雾包裹，日出那刻极可能看不见，提醒盯住云隙散开瞬间。
	res.ObscuredWarning = sunriseObscuredWarning(res.FogPeriods, eps, sunrise,
		cfg.Window.SunriseWindowBeforeMin, cfg.Window.SunriseWindowAfterMin, res.FogPotential)

	// 可信度：云海时次 + 时段数 + 模式垂直分辨率（机位上下相邻层间距）。
	// 只要有一段是「淹没型」（机位埋在云层顶部附近），可信度封顶「中」——
	// 人就在云里，能见度与稳定性都差，给「高/极高」是伪精度、会让人白跑。
	// （submerged 已在上方云海段计算，此处直接复用。）
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

// glowModels 是朝霞判定用于交叉验证的全模型集（含主模式 icon_seamless）。
// 与 mcpdiag glow-data 的默认四模型口径一致，覆盖 ICON/GFS/ECMWF/最佳融合，
// 专门用来抓「主模式（默认 ICON）系统性高估中层云」这类离群，
// 并让 sunset 口径以 GFS/ECMWF 为基准、把 ICON 当作离群排除。
var glowModels = []string{"icon_seamless", "gfs_seamless", "ecmwf_ifs025", "best_match"}

// DawnGlowContext 携带朝霞评估所需的辅助数据：AOD 与多模型共识输入。
// 单模型运行（无对比 / --no-cross-model）下可留空，退化为原单模型口径（回归安全）。
type DawnGlowContext struct {
	// PrimaryModel 是主模型标识（默认 icon_seamless，按 region 可能解析为 jma_msm 等），
	// 仅用于「分歧透明化」展示时给主模型打 (主) 标记；不参与判定逻辑。
	PrimaryModel string
	// AOD 是日出时刻的 CAMS 气溶胶光学厚度；Invalid 表示未取到（不降级）。
	AOD model.OptFloat
	// Compare 是辅助模式名 -> 该模式算出的朝霞档位（已含 AOD 降级）；不含主模式。
	// consensus/loose 口径使用它（与旧行为一致）；sunset 口径改用 Models。
	Compare map[string]string
	// Models 是全模型名 -> 该模式算出的朝霞档位（已含 AOD 降级），含主模式。
	// 由 runSunrise 一次性抓取全部 glowModels 后填充；sunset 口径据此以
	// GFS/ECMWF 为基准、排除 ICON 离群。为空时回落到 PrimaryModel+Compare（回归安全）。
	Models map[string]string
	// GlowPolicy 朝霞判定口径：空/"consensus" 走多模型共识封顶；"loose" 走
	// 主模型(ICON)与 ECMWF 取高（任一报大烧即采纳）；"sunset" 走 sunsetbot 口径
	// （GFS/ECMWF 取低、ICON 离群不参与）。
	GlowPolicy string
	// CrossSection 是「大气截面几何判定」的采样点（沿太阳方位角多点取数所得）。
	// 非空表示启用了该判定；BuildSunriseReport 据此施加几何门——
	// 沿太阳方向无任何云层可被曙/暮光照射时，封顶「无」并附几何原因。
	// 仅 --glow-cross-section 开启时由 runSunrise 填充。
	CrossSection []CrossSectionPoint
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

// looseDawnGlow 宽松口径：信任主模型(ICON)与 ECMWF 这两个高分辨率 NWP，
// 取二者最高档（任一报大烧即采纳），不施加多模型共识的多数封顶。
//
// 与 consensusDawnGlow 的「只降不升」相反，本函数是「取高」——用于用户想看
// 最乐观估计、自行承担离群风险的场景。各输入档位已含 AOD 降级与结构护栏，
// 因此宽松口径只去掉跨模型封顶，不会把物理上不可能的「厚阴天大烧」放宽出来。
// compare 为空（单模型）时 best 恒等于主模型结论，与共识行为一致。
func looseDawnGlow(primaryTier, primaryModel string, compare map[string]string) (string, string) {
	best := primaryTier
	bestModel := primaryModel
	if t, ok := compare["ecmwf_ifs025"]; ok {
		if dawnGlowTierRank(t) > dawnGlowTierRank(best) {
			best = t
			bestModel = "ecmwf_ifs025"
		}
	}
	if bestModel == primaryModel {
		return best, fmt.Sprintf("宽松口径（主%s 与 ECMWF 取高）：%s", primaryModel, best)
	}
	return best, fmt.Sprintf("宽松口径（主%s 与 ECMWF 取高）：ECMWF 判%s → %s", primaryModel, best, best)
}

// sunsetDawnGlow 实现 sunsetbot 口径：以 GFS/ECMWF 两个基准模型的一致性为准，
// 取二者较低档（任一方判无即无），彻底排除 ICON 系统性高估中层云的离群——
// ICON 既不参与抬高、也不参与压低主模型，只是透明展示用。
//
// 这是「用 sunsetbot 的方案优化朝霞」的核心：sunsetbot 本就用 GFS/ECMWF + CAMS AOD，
// 而非 ICON。绩溪 9-06 的根因正是 ICON 报 40% 中层云（大烧），而 GFS/ECMWF/最佳融合
// 都接近 0%（不烧）；旧 consensus 只是把 ICON 当主、多数封顶，sunset 直接改以
// GFS/ECMWF 为真理源，对齐 sunsetbot 的「不烧」结论。
//
// 单模型（models 为空，--no-cross-model）时退回主模型结论 + AOD，诚实不谎报。
// 最后叠加几何光照门 applyGlowGeoGate：评估时次太阳高度过低时云层尚未被照亮，封顶小烧。
func sunsetDawnGlow(primTier string, models map[string]string, aod model.OptFloat, sunElev float64) (string, string) {
	gfs, ecmwf := models["gfs_seamless"], models["ecmwf_ifs025"]

	// 单模型：无 GFS/ECMWF 对照，退回主模型 + AOD（诚实，不靠 ICON 离群）。
	if gfs == "" && ecmwf == "" {
		t, n := degradeDawnGlowByAOD(primTier,
			fmt.Sprintf("sunsetbot 口径（单模型，无 GFS/ECMWF 对照）：%s", primTier), aod)
		return applyGlowGeoGate(t, n, sunElev)
	}

	// 以 GFS/ECMWF 取低：二者任一判无即无；都不空时取较低档。
	anchor := gfs
	if anchor == "" {
		anchor = ecmwf
	}
	if ecmwf != "" && dawnGlowTierRank(ecmwf) < dawnGlowTierRank(anchor) {
		anchor = ecmwf
	}
	note := fmt.Sprintf("sunsetbot 口径（GFS/ECMWF 取低，ICON 离群不参与）：%s", anchor)
	t, n := degradeDawnGlowByAOD(anchor, note, aod)
	return applyGlowGeoGate(t, n, sunElev)
}

// applyGlowGeoGate 几何光照门：复刻 sunsetbot「阳光能否照到云底」的判定。
//
// 朝霞本质是日出前后阳光从下方/侧方照亮中高云层。若评估时次太阳地平高度过低
// （< −9°，仍在地球阴影里、尚未进入民用晨光），即便有云载体也无法被染红，
// 此时封顶小烧，避免对粗分辨率数据（3h 步长、最近整点落在日出前很久）误报大烧。
// 该门只降不升，且与 AOD 降级同向（都是「更诚实」），不吞掉真实大烧。
func applyGlowGeoGate(tier, note string, sunElev float64) (string, string) {
	if sunElev < -9 && dawnGlowTierRank(tier) > 1 {
		return "小烧", fmt.Sprintf("%s；⚠️几何门：评估时次太阳高度 %.0f° 过低、云层尚未被照亮，封顶小烧", note, sunElev)
	}
	return tier, note
}

// applySubmergedGlowFloor 淹没型云海（机位埋于云层顶部附近）的物理修正。
//
// 机位处就是云顶或紧贴云顶，日出时云顶必被晨光从上方/侧方染红——
// 这是确定的物理事实，不取决于任何数值模式的云量聚合。因此即便
// assessDawnGlow 因「只数中高云量、漏掉贴地云海」判了「无/小烧」，
// 这里也要把朝霞地板抬到「中烧」。
//
// 唯二不抬的例外（与 degradeDawnGlowByAOD 同向、只降不升）：
//   - AOD ≥ 1.0：天空浑浊、红光大部门被散射吞掉，淹没型也救不回；
//   - 档位已 ≥ 中烧：不重复操作。
// 该地板会覆盖前序的「几何门/截面门」结论——那些门沿太阳方向采样远处云层，
// 抓不到机位脚下这层贴身云海；本地淹没型是独立 guaranteed 的染红载体。
func applySubmergedGlowFloor(tier, note string, submerged bool, aod model.OptFloat) (string, string) {
	if !submerged {
		return tier, note
	}
	// AOD 极端：天空太浑，连贴身云海也染不出红，尊重 AOD 结论（通常为「无」）。
	if aod.Valid && aod.V >= 1.0 {
		return tier, note
	}
	if dawnGlowTierRank(tier) >= 2 {
		return tier, note // 已是中烧/大烧，无需抬
	}
	prefix := ""
	if note != "" {
		prefix = note + "；"
	}
	reason := prefix + "云海淹没型：机位埋于云顶附近，云顶必被晨光染红，朝霞至少中烧（几何/截面门低估了这层贴身云海）"
	return "中烧", reason
}

// sunriseObscuredWarning 判断日出拍摄窗口是否被云雾「100% 覆盖」，
// 触发「日出可能泡在雾里看不见」警告。覆盖两类情形：
//  1. 近地辐射雾时段（fogPeriods）完整盖住窗口；
//  2. 淹没型云海时段（云顶高过机位、机位埋在云里）完整盖住窗口。
//
// 两者都意味着机位处在云雾中，日出那刻极可能什么都看不见。
// 返回警告文案（空串=未触发）；文案内含「云隙」时机提示（云雾散开的时刻）。
// 窗口 = [日出 − beforeMin, 日出 + afterMin]，与云海抓拍窗口同参。
func sunriseObscuredWarning(fogPeriods []report.FogPeriod, eps []report.CloudSeaEpisode,
	sunrise time.Time, beforeMin, afterMin int, fogPotentialLevel string) string {

	// sunrise 是 FixedZone(+offset) 的当地墙钟（如 06:03 +0800），其绝对瞬间比墙钟早一个时区偏移；
	// 而 fp.Start / ep.Start 都是「UTC 承载的当地墙钟」（与 resp.Times 同口径）。直接比较会凭空差出
	// 一个时区偏移（本项目 +8h），窗口判定错跨日。先剥时区、只比墙钟
	// （与 episodeOverlapsWindow / cloudSeaCoversWindow / wallClockUTC 同口径）。
	sunWall := wallClockUTC(sunrise)
	winStart := sunWall.Add(-time.Duration(beforeMin) * time.Minute)
	winEnd := sunWall.Add(time.Duration(afterMin) * time.Minute)

	// 近地辐射雾：「触及」日出窗即触发（不再要求 100% 覆盖）。
	// 旧逻辑要求雾时段完整盖住 ±窗口，导致「峰值在午夜(02:00)、日出窗时段被模型判轻雾」
	// 的真实白雾场景漏报——报告写了「近地雾强」却不警告日出看不见。
	// 实测 9-06 牵牛岗：辐射雾 22:00–08:00 强覆盖日出窗，但模型把窗口段判轻雾使时段被截断，
	// 没有任何单段「完整包含」窗口 → 旧逻辑零警告。改为「与窗口有交叠即警告」后修复。
	// 诚实补充「地面可能一片白」：机位贴地、雾中时地面视野可能全白，日出那刻大概率看不见。
	// （按用户禁令不提无人机；只给「盯住云隙瞬间」的抓拍建议。）
	for _, fp := range fogPeriods {
		if fp.Start.Before(winEnd) && fp.End.After(winStart) {
			// 按当夜雾档峰值分级措辞：强雾贴地时地面视野可能全白，中雾则只是被遮挡。
			whiteout := "机位处可能被雾遮挡"
			if profile.FogLevelRank(fogPotentialLevel) >= profile.FogLevelRank(profile.FOG_STRONG) {
				whiteout = "机位处地面可能一片白"
			}
			return fmt.Sprintf(
				"日出可能泡在雾里看不见——近地辐射雾 %s–%s 覆盖日出窗（%s–%s），%s。盯住 %s 前后雾散/破云的云隙瞬间，才有机会拍到",
				fp.Start.Format("15:04"), fp.End.Format("15:04"),
				winStart.Format("15:04"), winEnd.Format("15:04"),
				whiteout, fp.End.Format("15:04"))
		}
	}
	// 淹没型云海：机位埋在云里，同样触发。End 即云顶破云/云海消散时刻。
	for _, ep := range eps {
		if !ep.Submerged {
			continue
		}
		if !ep.Start.After(winStart) && !ep.End.Before(winEnd) {
			return fmt.Sprintf(
				"日出可能泡在雾里看不见——淹没型云海 %s–%s 全程覆盖日出窗（%s–%s），机位处被云包裹。盯住 %s 前后云顶破云的云隙瞬间，才有机会拍到",
				ep.Start.Format("15:04"), ep.End.Format("15:04"),
				winStart.Format("15:04"), winEnd.Format("15:04"),
				ep.End.Format("15:04"))
		}
	}
	return ""
}

// dawnGlowDivergenceLabelFromMap 由全模型明细生成分歧一句话描述（透明化展示用）。
func dawnGlowDivergenceLabelFromMap(models map[string]string) string {
	if len(models) == 0 {
		return "单模型（无交叉验证）"
	}
	parts := make([]string, 0, len(models))
	for _, t := range models {
		parts = append(parts, t)
	}
	uniq := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		uniq[p] = struct{}{}
	}
	if len(uniq) == 1 {
		return fmt.Sprintf("模型一致：%s", parts[0])
	}
	maxRank, maxCount := 0, 0
	for _, p := range parts {
		switch r := dawnGlowTierRank(p); {
		case r > maxRank:
			maxRank, maxCount = r, 1
		case r == maxRank:
			maxCount++
		}
	}
	return fmt.Sprintf("模型分歧：仅 %d/%d 判%s", maxCount, len(parts), dawnGlowTierName(maxRank))
}

// orderedGlowModels 把全模型明细按固定顺序（icon→gfs→ecmwf→best）输出，
// 保证报告逐模型明细顺序稳定可读；sunset 口径下把 GFS 标记为基准(主)。
func orderedGlowModels(models map[string]string, glow DawnGlowContext) []report.DawnGlowModelVerdict {
	order := []string{"icon_seamless", "gfs_seamless", "ecmwf_ifs025", "best_match"}
	bd := make([]report.DawnGlowModelVerdict, 0, len(models))
	seen := make(map[string]bool, len(models))
	for _, m := range order {
		t, ok := models[m]
		if !ok {
			continue
		}
		// sunset 口径下，基准(主)是 GFS（锚定 sunsetbot），ICON 只是被透明展示的
		// 普通一行、不得标 (主)，否则会与 GFS 双双 (主) 误导读者；其余口径仍以
		// 主模型(PrimaryModel) 标 (主)。
		var primary bool
		if glow.GlowPolicy == "sunset" {
			primary = m == "gfs_seamless"
		} else {
			primary = m == glow.PrimaryModel
		}
		bd = append(bd, report.DawnGlowModelVerdict{Model: m, Tier: t, Primary: primary})
		seen[m] = true
	}
	for m, t := range models {
		if seen[m] {
			continue
		}
		bd = append(bd, report.DawnGlowModelVerdict{Model: m, Tier: t})
	}
	return bd
}

// dawnGlowDivergenceLabel 生成分歧一句话描述，供报告透明化展示。
// 全部一致时返回「模型一致：X」；出现分歧时返回「模型分歧：仅 k/n 判最高档」，
// 让用户一眼判断当前结论是否被多数模型支撑（而非只看共识封顶后的单一档位）。
func dawnGlowDivergenceLabel(primary string, compare map[string]string) string {
	if len(compare) == 0 {
		return "单模型（无交叉验证）"
	}
	parts := make([]string, 0, len(compare)+1)
	parts = append(parts, primary)
	for _, t := range compare {
		parts = append(parts, t)
	}
	uniq := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		uniq[p] = struct{}{}
	}
	if len(uniq) == 1 {
		return fmt.Sprintf("模型一致：%s", primary)
	}
	maxRank, maxCount := 0, 0
	for _, p := range parts {
		switch r := dawnGlowTierRank(p); {
		case r > maxRank:
			maxRank, maxCount = r, 1
		case r == maxRank:
			maxCount++
		}
	}
	return fmt.Sprintf("模型分歧：仅 %d/%d 判%s", maxCount, len(parts), dawnGlowTierName(maxRank))
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

// assessDawnGroundFogPeriods 给出日出夜间「辐射雾时段」列表，与云海判定完全独立。
//
// 窗口 = [日出 − 8h, 日出 + 2h]，覆盖辐射雾的生成（前半夜）→ 最重（天亮前）→ 消散（日出后），
// 比 assessDawnGroundFog 的紧凑拍摄窗口更宽——后者只取「现场按快门那段时间」的最强档，
// 而时段需要看整夜的演变。逐时调用 profile.AssessGroundFog，把雾档 ≥ FOG_MODERATE（中，
// 地面雾可拍）的连续小时聚合成时段，并记录时段内峰值档位与峰值时刻。
//
// 全窗口均 < 中（只有轻雾/无雾）时返回 nil；时间轴为空也返回 nil。
// 判定一律由 profile.AssessGroundFog 给出，此处只负责挑时次与聚合，不复制任何判据。
// 与 assessDawnGroundFog 同理：比较前用 wallClockUTC 统一剥时区，避免窗口整体平移。
func assessDawnGroundFogPeriods(resp *api.Response, sunrise time.Time, cfg config.Config) []report.FogPeriod {
	if resp == nil || len(resp.Times) == 0 {
		return nil
	}
	const (
		beforeH = 8
		afterH  = 2
		minRank = 2 // FOG_MODERATE(中) 及以上才计入时段；弱(轻雾)不构成可拍成片雾
	)
	winBefore := time.Duration(beforeH) * time.Hour
	winAfter := time.Duration(afterH) * time.Hour
	sunriseWall := wallClockUTC(sunrise)

	type hourFog struct {
		t     time.Time
		level string
	}
	var hours []hourFog
	for idx, localDT := range resp.Times {
		delta := wallClockUTC(localDT).Sub(sunriseWall)
		if delta > winAfter || delta < -winBefore {
			continue
		}
		a := profile.AssessGroundFog(resp.Surface(idx), cfg.Thresh)
		hours = append(hours, hourFog{localDT, a.Level})
	}
	if len(hours) == 0 {
		return nil
	}

	var periods []report.FogPeriod
	run := make([]hourFog, 0, len(hours))
	flush := func() {
		if len(run) == 0 {
			return
		}
		start, end := run[0].t, run[len(run)-1].t
		peakLevel := profile.FOG_NONE
		var peakHour time.Time
		peakRank := -1
		for _, h := range run {
			// 峰值取最强档；并列时取该时段内最早出现的那一小时（天亮前最重的典型形态）。
			if r := profile.FogLevelRank(h.level); r > peakRank {
				peakRank, peakLevel, peakHour = r, h.level, h.t
			}
		}
		periods = append(periods, report.FogPeriod{
			Start:     start,
			End:       end.Add(time.Hour), // End 为消散时刻（含至 End 前一小时）
			PeakLevel: peakLevel,
			PeakHour:  peakHour,
		})
		run = run[:0]
	}
	for _, h := range hours {
		if profile.FogLevelRank(h.level) >= minRank {
			run = append(run, h)
		} else {
			flush()
		}
	}
	flush()
	if len(periods) == 0 {
		return nil
	}
	return periods
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
