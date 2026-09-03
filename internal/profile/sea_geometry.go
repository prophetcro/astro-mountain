package profile

import "github.com/prophetcro/astro-mountain/internal/config"

// 云海形态标签。
const (
	// SEA_NONE 没有可拍的云海形态（零值，Present 必为 false）。
	SEA_NONE = ""
	// SEA_BELOW 脚下型：云顶严格低于机位，头顶通透或只剩一层薄云。
	// 这是最标准、最稳的云海形态，可放心守候。
	SEA_BELOW = "below"
	// SEA_SUBMERGED 淹没型：机位埋在云层顶部附近，脚下是厚云海、头顶只剩薄云。
	// 高山云海的典型形态（牛草山、牵牛岗这类常年云海点位），
	// 可守候云隙破云，但机位本身在云中、能见度与稳定性都差。
	SEA_SUBMERGED = "submerged"
)

// SeaGeometry 机位与云海的几何关系，是「是否存在可拍云海」的唯一权威口径。
//
// 它由三处共用，任何一处自行实现都会造成口径分叉：
//   - profile.EvaluateHour               逐小时评级（REL_SEA_BELOW / REL_SEA_BELOW_IN_CLOUD）
//   - core.CollectCloudSeaEpisodesForNight 云海时段检测
//   - core.AnalyseSite                   逐小时「云海 有/无」列
//
// 历史教训（2026-09）：评级器承认脚下型/淹没型/薄云顶型三种形态，
// 而云海时段检测与 AnalyseSite 只认「云顶严格低于机位」一种。
// 结果淹没型（云从山脚一路堆过机位、脚下无独立层）被**系统性漏检**：
// 真实数据 20 站点 1620 个夜窗时次中漏掉 185 个，比实际检出的 142 个还多。
// 用户"用了这么久一次云海都没出现但实际上是有的"正源于此。
type SeaGeometry struct {
	// Present 是否存在可拍的云海形态。
	Present bool
	// Kind 形态标签：SEA_BELOW / SEA_SUBMERGED / SEA_NONE。
	Kind string

	// TopMSL 云顶海拔（米）。脚下型取脚下云顶最高那层的顶；
	// 淹没型取包裹机位那层的顶（高于机位）。
	TopMSL float64
	// TopAGL 云顶相对机位的高度（米）：脚下型为正（机下 Xm），
	// 淹没型为负（云顶高出机位 Xm）。
	TopAGL float64
	// BelowBase 脚下云厚（米）= 机位海拔 − 云底海拔。
	BelowBase float64
	// AboveTop 头顶云厚（米）= 云顶海拔 − 机位海拔；脚下型为 0，淹没型为正。
	AboveTop float64
	// Thickness 该云层厚度（米）。
	Thickness float64
}

// ClassifySeaGeometry 判定机位与云海的几何关系。
//
// 判定顺序与 EvaluateHour 的评级语义严格一致，三种形态按序命中：
//  1. REL_SEA_BELOW 脚下型 —— 云顶严格低于机位；
//  2. REL_IN_CLOUD 淹没型 —— 机位埋在云里，但脚下云够厚（≥ CloudSeaBeneathDepthM）
//     且头顶云够薄（≤ CloudSeaAboveDepthM），仍是高山云海而非坐实埋云；
//  3. REL_OVERHEAD 薄云顶型 —— 脚下有云海、头顶云层薄到不会真挡住，
//     云海形态不被头顶薄云一票否决。
//
// 都不命中即无可拍云海（Present=false）。纯 REL_IN_CLOUD（脚下云不够厚）
// 也返回 Present=false —— 那是真的埋在厚云里，不该算云海。
func ClassifySeaGeometry(siteAlt float64, layers []CloudLayer,
	t config.Thresholds) SeaGeometry {

	if len(layers) == 0 {
		return SeaGeometry{}
	}
	relation, keyLayer := ClassifySite(siteAlt, layers)
	if keyLayer == nil {
		return SeaGeometry{}
	}

	switch relation {
	case REL_SEA_BELOW:
		// keyLayer 就是脚下云顶最高的那层（ClassifySite 已按此挑出）。
		return SeaGeometry{
			Present:   true,
			Kind:      SEA_BELOW,
			TopMSL:    keyLayer.TopMSL,
			TopAGL:    siteAlt - keyLayer.TopMSL,
			BelowBase: siteAlt - keyLayer.BaseMSL,
			AboveTop:  0,
			Thickness: keyLayer.Thickness(),
		}

	case REL_IN_CLOUD:
		belowBase := siteAlt - keyLayer.BaseMSL
		aboveTop := keyLayer.TopMSL - siteAlt
		if belowBase >= t.CloudSeaBeneathDepthM && aboveTop <= t.CloudSeaAboveDepthM {
			return SeaGeometry{
				Present:   true,
				Kind:      SEA_SUBMERGED,
				TopMSL:    keyLayer.TopMSL,
				TopAGL:    siteAlt - keyLayer.TopMSL, // 负值：云顶高出机位
				BelowBase: belowBase,
				AboveTop:  aboveTop,
				Thickness: keyLayer.Thickness(),
			}
		}
		return SeaGeometry{}

	default:
		// REL_OVERHEAD：脚下同时有云海、且头顶云层薄到不会真的挡住时，
		// 云海形态成立。BelowBase 取脚下那层的底，不是头顶这层的。
		below, ok := HighestBeneathLayer(siteAlt, layers)
		if !ok || keyLayer.Thickness() > t.CloudSeaAboveDepthM {
			return SeaGeometry{}
		}
		return SeaGeometry{
			Present:   true,
			Kind:      SEA_BELOW,
			TopMSL:    below.TopMSL,
			TopAGL:    siteAlt - below.TopMSL,
			BelowBase: siteAlt - below.BaseMSL,
			AboveTop:  keyLayer.TopMSL - siteAlt,
			Thickness: below.Thickness(),
		}
	}
}
