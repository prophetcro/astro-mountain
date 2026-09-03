package profile

import (
	"strings"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// 近地体积雾（辐射雾）档位。
//
// 与观星模式的 applyFogVeto 是两条互不干扰的线，不要合并：
//   - applyFogVeto 把雾当「干扰」否决掉（起雾 = 看不见星星），维持原样不动；
//   - 这里的档位把近地雾当「拍摄主体」正面提示——对云海/朝霞摄影来说，
//     日出时分的贴地辐射雾本身就是可拍的题材（佘山天马望远镜那类画面），
//     报告只说「无云海 + 大烧朝霞」时用户无法判断现场有没有地面雾。
//
// 因此两者阈值与措辞完全独立，本包不修改、不复用 applyFogVeto 的任何输出。
const (
	// FOG_NONE 近地未达成雾条件（零值语义，渲染层据此跳过该行）。
	FOG_NONE = "无"
	// FOG_WEAK 近地接近饱和或有轻雾，值得留意但不必专程。
	FOG_WEAK = "弱"
	// FOG_MODERATE 轻雾/霾量级（能见度 < haze_visibility_m），地面雾可拍。
	FOG_MODERATE = "中"
	// FOG_STRONG 雾量级（能见度 < fog_visibility_m），近地体积雾大概率成片。
	FOG_STRONG = "强"
)

// fogLevelOrder 是档位由弱到强的全序，供 FogLevelRank 与 shiftFogLevel 换算。
// 顺序即语义：无 < 弱 < 中 < 强。
var fogLevelOrder = [4]string{FOG_NONE, FOG_WEAK, FOG_MODERATE, FOG_STRONG}

// 近地雾阈值的内置兜底值。
//
// config.json 里新增的阈值若缺失（字段为 0）或被写成负数，一律退回这里的默认值，
// 保证任何配置下都能给出档位——否则「阈值 = 0」会把「无」误判成「强」
// （RH 恒 ≥ 0、温露差恒 ≤ 0 都成立），那是比缺测更糟的伪精度。
const (
	defaultFogVisibilityM        = 1000.0
	defaultHazeVisibilityM       = 5000.0
	defaultFogProxyRHStrong      = 95.0
	defaultFogProxyRHModerate    = 90.0
	defaultFogProxyRHHigh        = 98.0
	defaultFogProxyRHWarn        = 95.0
	defaultFogProxySpreadStrongC = 2.0
	defaultFogProxySpreadModC    = 4.0
	defaultFogWindOptimalMinMS   = 1.0
	defaultFogWindOptimalMaxMS   = 3.0
	defaultFogWindDisruptMS      = 5.0
	defaultFogClearSkyLowCC      = 20.0
	defaultFogGlowMidHighCC      = 40.0
)

// FogAssessment 近地体积雾的档位与理由说明，供报告直接展示。
type FogAssessment struct {
	// Level 档位：强 / 中 / 弱 / 无。
	Level string
	// Note 人话理由：先列证据（地面RH / 温露差 / 风速 / 能见度），
	// 再写辐射雾的加成或抑制理由；能见度缺测时明写「按近地 RH 代理判定」。
	Note string
}

// FogLevelRank 把档位换算成可比权重（无=0 < 弱 < 中 < 强），供聚合时取最强档。
// 未知档位一律返回 0（等同「无」），绝不让它盖过真实判定。
func FogLevelRank(level string) int {
	for i, l := range fogLevelOrder {
		if l == level {
			return i
		}
	}
	return 0
}

// AssessGroundFog 判定单时次的近地体积雾（辐射雾）可能档位。
//
// 判定优先级——能见度是权威，缺测才降级到代理判据：
//  1. 能见度有效：< fog_visibility_m(默认1000m) → 强；< haze_visibility_m(默认5000m) → 中；
//     能见度很好时只在近地确实接近饱和时才给「弱」，否则「无」。
//  2. 能见度缺测：回落到「近地 RH + 温露差」的代理判据（proxyFogLevel），
//     并在 Note 里写明是按代理判定的，绝不假装知道能见度。
//
// 最后叠加辐射雾修正（radiationFogAdjust）：风速与低云量只在已有雾信号时微调一档，
// 不凭空给「无」造信号，也不把「弱」直接抬成「强」。
func AssessGroundFog(s model.Surface, t config.Thresholds) FogAssessment {
	vis := s.Visibility
	rh := s.RelativeHumidity2m
	spread := model.Sub(s.Temperature2m, s.DewPoint2m)

	level := FOG_NONE
	byProxy := false
	switch {
	case vis.Valid && vis.V < fogThreshold(t.FogVisibilityM, defaultFogVisibilityM):
		level = FOG_STRONG
	case vis.Valid && vis.V < fogThreshold(t.HazeVisibilityM, defaultHazeVisibilityM):
		level = FOG_MODERATE
	case vis.Valid:
		// 能见度在霾档之上：只有近地确实接近饱和才给「弱」，否则如实判「无」。
		if proxyFogLevel(rh, spread, t) != FOG_NONE {
			level = FOG_WEAK
		}
	default:
		// 能见度缺测：代理判据是唯一依据，可信度低于能见度，Note 里必须写明。
		level = proxyFogLevel(rh, spread, t)
		byProxy = true
	}

	note := fogEvidenceText(s, vis.Valid)
	// appendSeg 追加一段理由；证据串为空（近地要素全缺测）时不留前导分隔符。
	appendSeg := func(seg string) {
		if seg == "" {
			return
		}
		if note == "" {
			note = seg
			return
		}
		note = note + "；" + seg
	}

	// 辐射雾修正：只在已有雾信号（非「无」）时生效，最多改一档。
	adjust, reason := radiationFogAdjust(level, s, t)
	if adjust != 0 {
		level = shiftFogLevel(level, adjust)
	}
	appendSeg(reason)

	// 能见度封顶：加成/抑制都不能推翻能见度这个权威证据。
	// 能见度 20km 却被「风速有利 + 晴空」加成抬成「中」是典型的伪精度——
	// 用户会照着「中」跑一趟，结果现场根本没有雾。
	if cap, ok := fogVisibilityCap(vis, t); ok && FogLevelRank(level) > FogLevelRank(cap) {
		level = cap
		appendSeg("但能见度 " + itoa(model.RoundToInt(vis.V)) +
			"m 未达该档证据，档位封顶「" + cap + "」（加成不得推翻能见度证据）")
	}

	if byProxy {
		// 能见度缺测必须写进理由：代理判据的可信度低于能见度，用户有权知道。
		// 连 RH 也缺测时就没有可用代理了，如实说「无法判定」，档位维持「无」。
		if rh.Valid {
			appendSeg("能见度缺测，按近地 RH 代理判定")
		} else {
			appendSeg("能见度与近地 RH 均缺测，无法判定近地雾")
		}
	}
	return FogAssessment{Level: level, Note: note}
}

// fogVisibilityCap 给出能见度作为权威证据对档位的上限。
//
// 能见度是判雾的第一证据，辐射雾加成只能在这个上限之内微调：
//   - 能见度良好（≥ haze_visibility_m）→ 上限「弱」：只保留「近地接近饱和」的弱提示；
//   - 能见度在霾档内（≥ fog_visibility_m 但 < haze_visibility_m）→ 上限「中」：
//     轻雾可拍，但没有成片雾的证据，不该给「强」；
//   - 能见度在雾档内（< fog_visibility_m）或能见度缺测 → 无上限（交给代理判据与加成）。
//
// ok=false 表示不设上限。
func fogVisibilityCap(vis model.OptFloat, t config.Thresholds) (string, bool) {
	if !vis.Valid {
		return FOG_NONE, false
	}
	haze := fogThreshold(t.HazeVisibilityM, defaultHazeVisibilityM)
	fog := fogThreshold(t.FogVisibilityM, defaultFogVisibilityM)
	switch {
	case vis.V >= haze:
		return FOG_WEAK, true
	case vis.V >= fog:
		return FOG_MODERATE, true
	default:
		return FOG_NONE, false
	}
}

// proxyFogLevel 用近地相对湿度与温露差作代理判据，是能见度缺测时的唯一依据。
//
// 两个条件必须同时满足才算对应档位——温露差反映「离饱和还有多远」，
// RH 反映「现在已经有多湿」，只看一个都会误判：
// RH 高但温露差大说明已经暖起来了；温露差小但 RH 低说明是干冷的高空。
//
//   - 温露差 ≤ fog_proxy_spread_strong_c(2℃) 且 RH ≥ fog_proxy_rh_strong(95%) → 强
//   - 温露差 ≤ fog_proxy_spread_moderate_c(4℃) 且 RH ≥ fog_proxy_rh_moderate(90%) → 中
//   - 温露差或 RH 单方面接近上述门槛 → 弱
//
// 只有 RH 有效（温度或露点缺测）时退化为纯 RH 判据，沿用观星否决项同一套 RH 阈值口径。
// 两者都缺测 → 「无」，不编造。
func proxyFogLevel(rh, spread model.OptFloat, t config.Thresholds) string {
	rhStrong := fogThreshold(t.FogProxyRHStrong, defaultFogProxyRHStrong)
	rhModerate := fogThreshold(t.FogProxyRHModerate, defaultFogProxyRHModerate)
	spreadStrong := fogThreshold(t.FogProxySpreadStrongC, defaultFogProxySpreadStrongC)
	spreadModerate := fogThreshold(t.FogProxySpreadModerateC, defaultFogProxySpreadModC)

	if rh.Valid && spread.Valid {
		switch {
		case spread.V <= spreadStrong && rh.V >= rhStrong:
			return FOG_STRONG
		case spread.V <= spreadModerate && rh.V >= rhModerate:
			return FOG_MODERATE
		case spread.V <= spreadModerate || rh.V >= rhModerate:
			return FOG_WEAK
		}
		return FOG_NONE
	}

	if rh.Valid {
		switch {
		case rh.V >= fogThreshold(t.FogProxyRHHigh, defaultFogProxyRHHigh):
			return FOG_STRONG
		case rh.V >= fogThreshold(t.FogProxyRHWarn, defaultFogProxyRHWarn):
			return FOG_MODERATE
		case rh.V >= rhModerate:
			return FOG_WEAK
		}
	}
	return FOG_NONE
}

// radiationFogAdjust 给出辐射雾的加成/抑制（返回档位偏移与理由）。
//
// 物理依据：
//   - 风速 1~3 m/s 最利于辐射雾：有轻微扰动把近地湿层抬到凝结高度，又不至于吹散；
//     过静（< 1 m/s）常常只结露不成雾；> 5 m/s 的湍流会破坏逆温层，把雾吹散/抬成低云。
//   - 低云量低（晴空）利于夜间地面辐射降温 → 加成。
//   - 朝霞需要中高云载体，所以「低云少 + 中高云适中」正是雾与朝霞同框的窗口，
//     命中时单独点出来，这是用户真正想守的画面。
//
// 两条硬约束（防止修正变成新的伪精度）：
//  1. 档位为「无」时完全不修正——本来没有任何雾的证据，加成不该凭空造信号；
//  2. 累计偏移夹在 [-1, +1]，一次最多改一档，绝不把「弱」直接抬成「强」。
func radiationFogAdjust(level string, s model.Surface, t config.Thresholds) (int, string) {
	if level == FOG_NONE {
		return 0, ""
	}

	optMin := fogThreshold(t.FogWindOptimalMinMS, defaultFogWindOptimalMinMS)
	optMax := fogThreshold(t.FogWindOptimalMaxMS, defaultFogWindOptimalMaxMS)
	disrupt := fogThreshold(t.FogWindDisruptMS, defaultFogWindDisruptMS)
	clearSkyCC := fogThreshold(t.FogClearSkyLowCC, defaultFogClearSkyLowCC)
	glowCC := fogThreshold(t.RadiationFogMidHighCC, defaultFogGlowMidHighCC)

	adjust := 0
	reasons := make([]string, 0, 2)

	wind := s.WindSpeed10m
	if wind.Valid {
		switch {
		case wind.V >= optMin && wind.V <= optMax:
			adjust++
			reasons = append(reasons, "风速 "+model.FormatFixed(wind.V, 1)+
				"m/s 处于辐射雾最有利区间（"+model.FormatFixed(optMin, 1)+"~"+
				model.FormatFixed(optMax, 1)+"m/s），利于辐射雾形成")
		case wind.V > disrupt:
			adjust--
			reasons = append(reasons, "风速 "+model.FormatFixed(wind.V, 1)+
				"m/s 偏大（>"+model.FormatFixed(disrupt, 1)+"m/s），湍流破坏逆温层，雾易被吹散，档位下调")
		case wind.V < optMin:
			// 近乎静风：过静往往只结露不成雾，不上调也不下调，只如实说明。
			reasons = append(reasons, "近地近乎静风（"+model.FormatFixed(wind.V, 1)+
				"m/s），更可能只结露不成雾")
		}
	}

	low := s.CloudCoverLow
	if low.Valid && low.V <= clearSkyCC {
		adjust++
		midhigh := 0.0
		if s.CloudCoverMid.Valid && s.CloudCoverMid.V > midhigh {
			midhigh = s.CloudCoverMid.V
		}
		if s.CloudCoverHigh.Valid && s.CloudCoverHigh.V > midhigh {
			midhigh = s.CloudCoverHigh.V
		}
		if midhigh > 0 && midhigh >= glowCC {
			reasons = append(reasons, "低云量 "+model.FormatFixed(low.V, 0)+
				"% 偏低、中高云 "+model.FormatFixed(midhigh, 0)+
				"% 适中，低云少＋中高云适中正是雾与朝霞同框的窗口")
		} else {
			reasons = append(reasons, "低云量 "+model.FormatFixed(low.V, 0)+
				"% 偏低，晴空利于夜间辐射降温")
		}
	}

	if adjust > 1 {
		adjust = 1
	}
	if adjust < -1 {
		adjust = -1
	}
	return adjust, strings.Join(reasons, "；")
}

// shiftFogLevel 在全序上平移档位，越界时夹到两端（不会绕回，也不会溢出成空串）。
func shiftFogLevel(level string, delta int) string {
	idx := FogLevelRank(level) + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(fogLevelOrder) {
		idx = len(fogLevelOrder) - 1
	}
	return fogLevelOrder[idx]
}

// fogEvidenceText 拼装证据串：地面RH、温露差、风速、能见度。
// 缺测的要素一律不写（不假装知道）；能见度缺测由调用方在理由里统一说明，此处留空。
func fogEvidenceText(s model.Surface, visValid bool) string {
	parts := make([]string, 0, 5)
	if rh := s.RelativeHumidity2m; rh.Valid {
		parts = append(parts, "地面RH "+model.FormatFixed(rh.V, 0)+"%")
	}
	if spread := model.Sub(s.Temperature2m, s.DewPoint2m); spread.Valid {
		parts = append(parts, "温露差 "+model.FormatFixed(spread.V, 1)+"℃")
	}
	if wind := s.WindSpeed10m; wind.Valid {
		parts = append(parts, "风速 "+model.FormatFixed(wind.V, 1)+"m/s")
	}
	if visValid {
		parts = append(parts, "能见度 "+itoa(model.RoundToInt(s.Visibility.V))+"m")
	}
	return strings.Join(parts, "、")
}

// fogThreshold 取配置阈值，缺失（≤0）时退回内置默认值。
// 所有近地雾判定都必须经它取阈值，杜绝「配置文件未升级 → 阈值 0 → 全判最强」的静默事故。
func fogThreshold(v, def float64) float64 {
	if v > 0 {
		return v
	}
	return def
}
