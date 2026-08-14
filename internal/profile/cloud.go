package profile

import (
	"math"

	"github.com/prophetcro/astro-mountain/internal/config"
)

// interpBoundary 在相邻两层之间线性插值出云的边界高度。
//
// 只要有一侧的云量达到阈值，就沿云量插值到该阈值；否则说明这层云是靠湿度判出来的，
// 改沿相对湿度插值到「有云那一侧」的湿度阈值。两侧取值几乎相等时无法定向，
// 取中点。插值比例夹在 [0,1]，保证边界不会跑到两层之外。
func interpBoundary(lower, upper Level, t config.Thresholds) float64 {
	ccThr := t.CloudCoverThreshold

	var a, b, target float64
	if lower.CCV() >= ccThr || upper.CCV() >= ccThr {
		a, b, target = lower.CCV(), upper.CCV(), ccThr
	} else {

		cloudySide := lower
		if upper.Cloudy(t) {
			cloudySide = upper
		}
		a, b, target = lower.RHV(), upper.RHV(), cloudySide.RHThreshold(t)
	}

	var frac float64
	if isClose(a, b) {
		frac = 0.5
	} else {
		frac = (target - a) / (b - a)
	}
	frac = math.Min(1.0, math.Max(0.0, frac))
	return lower.Height + frac*(upper.Height-lower.Height)
}

// isClose 按相对容差 1e-09 判断两个浮点是否可视为相等，
// 无穷大只在完全相等时才算接近。
func isClose(a, b float64) bool {
	const relTol = 1e-09
	if a == b {
		return true
	}
	if math.IsInf(a, 0) || math.IsInf(b, 0) {
		return false
	}
	diff := math.Abs(a - b)
	return diff <= relTol*math.Max(math.Abs(a), math.Abs(b))
}

// DetectLayers 把按高度升序的廓线切成若干云层。
//
// 做法是找出连续「有云」的层段，再向相邻的无云层插值出云底与云顶。
// 层段顶到剖面边界时无从插值，直接取端点高度并标记 OpenBase/OpenTop，
// 表示真实边界在剖面之外、厚度只是下限。
//
// 传入的 levels 必须按高度升序（BuildProfile 的输出即满足），
// 否则层段与边界都会算错。
func DetectLayers(levels []Level, t config.Thresholds) []CloudLayer {
	if len(levels) == 0 {
		return nil
	}

	flags := make([]bool, len(levels))
	for i, lv := range levels {
		flags[i] = lv.Cloudy(t)
	}

	var layers []CloudLayer
	frac := t.LayerMinHalfSpanFrac
	n := len(levels)

	for i := 0; i < n; i++ {
		if !flags[i] {
			continue
		}
		// 吃掉整段连续有云的层，[start, end] 即一层云。
		start := i
		for i+1 < n && flags[i+1] {
			i++
		}
		end := i

		var base float64
		openBase := false
		if start > 0 {
			base = interpBoundary(levels[start-1], levels[start], t)
			// 下界不越过下面那个无云层；同时至少向下延伸层间距的 frac，
			// 免得插值把云层压成一张零厚度的纸。
			base = math.Max(base, levels[start-1].Height)

			base = math.Min(base, levels[start].Height-
				frac*(levels[start].Height-levels[start-1].Height))
		} else {
			base = levels[start].Height
			openBase = true
		}

		var top float64
		openTop := false
		if end < n-1 {
			// 上界同理：不越过上面那个无云层，且至少向上延伸层间距的 frac。
			top = interpBoundary(levels[end], levels[end+1], t)
			top = math.Min(top, levels[end+1].Height)
			top = math.Max(top, levels[end].Height+
				frac*(levels[end+1].Height-levels[end].Height))
		} else {
			top = levels[end].Height
			openTop = true
		}

		maxCC := levels[start].CCV()
		maxRH := levels[start].RHV()
		for _, lv := range levels[start : end+1] {
			if v := lv.CCV(); v > maxCC {
				maxCC = v
			}
			if v := lv.RHV(); v > maxRH {
				maxRH = v
			}
		}

		layers = append(layers, CloudLayer{
			BaseMSL: base,
			// 兜底保证云顶不低于云底，厚度恒非负。
			TopMSL:   math.Max(top, base),
			MaxCC:    maxCC,
			MaxRH:    maxRH,
			OpenTop:  openTop,
			OpenBase: openBase,
		})
	}
	return layers
}
