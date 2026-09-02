// Package profile 从气压层廓线反演云层结构，并给出逐小时的通透性评级。
//
// 数据流：原始气压层要素 → BuildProfile 整理成按高度升序的 Level 列表 →
// DetectLayers 反演出成层的 CloudLayer → ClassifySite 判断云与机位的相对关系 →
// EvaluateHour 结合地面要素给出评级与人话说明。
//
// 这是 core 评级内核的「看云」部分：本包只做判断，不负责取数与渲染。
package profile

import (
	"sort"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// BuildProfile 把按气压索引的原始层要素整理成可用的廓线。
//
// 处理三件事：位势高缺失时回落到标准大气高度；丢弃低于
// t.MinLevelHeightMSL 的层与云量、湿度全缺的层；按高度升序排列。
// 高度相差不足 1 m 的层视为同一层，只保留 CC+RH 之和更大的那个，
// 避免同一高度出现重复层干扰后续的成层检测。
func BuildProfile(levelValues map[int]model.RawLevel, t config.Thresholds) []Level {
	levels := make([]Level, 0, len(PressureLevels))
	for _, pressure := range PressureLevels {
		raw := levelValues[pressure]

		height := raw.GH.Or(StdHeightM[pressure])
		if height < t.MinLevelHeightMSL {
			continue
		}
		lv := Level{
			Pressure: pressure,
			Height:   height,
			CC:       raw.CC,
			RH:       raw.RH,
		}
		if !lv.Known() {
			continue
		}
		levels = append(levels, lv)
	}

	sort.SliceStable(levels, func(i, j int) bool {
		return levels[i].Height < levels[j].Height
	})

	deduped := make([]Level, 0, len(levels))
	for _, lv := range levels {
		if n := len(deduped); n > 0 && lv.Height-deduped[n-1].Height < 1.0 {
			if lv.CCV()+lv.RHV() > deduped[n-1].CCV()+deduped[n-1].RHV() {
				deduped[n-1] = lv
			}
			continue
		}
		deduped = append(deduped, lv)
	}
	return deduped
}

// CountAbove 统计严格高于机位海拔的层数，用于衡量云底/云顶反演的分辨率：
// 机位以上的层太少时，反演结果的置信度有限。
func CountAbove(levels []Level, siteAlt float64) int {
	n := 0
	for _, lv := range levels {
		if lv.Height > siteAlt {
			n++
		}
	}
	return n
}

// MaxCCBelow 返回不高于 maxHeight 的层中的最大云量；
// 该范围内没有任何有效云量时返回缺测（而不是 0）。
// 用作 API 未直接给出低云量时的替代来源。
func MaxCCBelow(levels []Level, maxHeight float64) model.OptFloat {
	out := model.Missing()
	for _, lv := range levels {
		if lv.Height <= maxHeight && lv.CC.Valid {
			if !out.Valid || lv.CC.V > out.V {
				out = model.Num(lv.CC.V)
			}
		}
	}
	return out
}

// MaxGapAroundSite 找出覆盖机位的「上下相邻两层」之间的最大间距（米）。
//
// 返回 0 表示机位恰好落在某层上或紧贴某层，没有插值盲区。
// 返回 >500m 意味着机位海拔正好嵌在一段没有数据的真空中
// （典型场景：ECMWF/JMA 只提供 1000/925/850/700 四层，925↔850 间距 ≈ 732m；
// 8 层模式下 900↔850 间距 ≈ 493m），云海判定置信度有限。
//
// 11 层模式（加 875/825/750）下盲区已降到 ≤260m，
// 故「数据垂直分辨率不足」的告警阈值取 500m：宽于 8 层最好值，
// 严于 8 层最差值，对 ECMWF/JMA 仍能正确告警。
func MaxGapAroundSite(levels []Level, siteAlt float64) float64 {
	if len(levels) < 2 {
		return 0
	}
	maxGap := 0.0
	for i := 1; i < len(levels); i++ {
		lower, upper := levels[i-1], levels[i]
		// 机位严格夹在两层之间才算盲区：等号命中（机位与某层同高）不计入，
		// 因为该层自身的位势高/云量就能覆盖机位，无需跨层插值。
		if lower.Height < siteAlt && siteAlt < upper.Height {
			gap := upper.Height - lower.Height
			if gap > maxGap {
				maxGap = gap
			}
		}
	}
	return maxGap
}
