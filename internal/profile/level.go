package profile

import (
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// PressureLevels 是参与反演的气压层（hPa），由低空到高空排列。
//
// 必须与 internal/api.PressureLevels 保持一致——前者是反演端，后者是取数端。
// 任何一处改动必须同步另一处，否则会出现"取到了但反演层缺"或"反演层有但取不到"的不一致。
//
// 2026-09 升级到 11 层（增 875/825/750），原因见 internal/api/vars.go 同名变量注释。
var PressureLevels = [...]int{1000, 975, 950, 925, 900, 875, 850, 825, 800, 750, 700}

// StdHeightM 是各气压层的标准大气近似高度（米，海拔）。
// 仅在模式没给出该层位势高时兜底使用，精度弱于实测位势高。
//
// 数值来自标准大气压高公式 h = 44330 * (1 - (P/1013.25)^(1/5.255))，
// 与旧 8 层数值同源，加 875/825/750 时保持公式一致性。
var StdHeightM = map[int]float64{
	1000: 110.0, 975: 320.0, 950: 540.0, 925: 760.0,
	900: 990.0, 875: 1220.0, 850: 1460.0, 825: 1700.0, 800: 1950.0,
	750: 2510.0, 700: 3010.0,
}

// Level 是单个气压层的廓线取值：高度（米，海拔）、云量与相对湿度均可缺测。
type Level struct {
	Pressure int
	Height   float64
	CC       model.OptFloat
	RH       model.OptFloat
}

// Known 判断这一层是否至少有一个可用要素；两者全缺的层不参与反演。
func (l Level) Known() bool { return l.CC.Valid || l.RH.Valid }

// CCV 返回云量，缺测按 0 处理，供比较与插值使用。
func (l Level) CCV() float64 { return l.CC.Or(0.0) }

// RHV 返回相对湿度，缺测按 0 处理，供比较与插值使用。
func (l Level) RHV() float64 { return l.RH.Or(0.0) }

// RHThreshold 返回该层「算不算云」的相对湿度阈值。
// 低空层（气压不低于 t.RHLowLayerPressureMin）与高空层用不同阈值。
func (l Level) RHThreshold(t config.Thresholds) float64 {
	if l.Pressure >= t.RHLowLayerPressureMin {
		return t.RHThresholdLow
	}
	return t.RHThresholdHigh
}

// Cloudy 判断该层是否算有云。
//
// 云量是第一判据；只有当云量缺测时，才退而用相对湿度代理判断。
// 换言之，模式明确给了低云量就以它为准，不会被高湿度反推成有云。
func (l Level) Cloudy(t config.Thresholds) bool {
	if l.CC.Valid && l.CC.V >= t.CloudCoverThreshold {
		return true
	}
	if !l.CC.Valid && l.RH.Valid && l.RH.V >= l.RHThreshold(t) {
		return true
	}
	return false
}

// CloudLayer 是一段连续有云层段反演出的云层：
// BaseMSL/TopMSL 为云底、云顶海拔（米），MaxCC/MaxRH 为层内最大云量与湿度。
// OpenBase/OpenTop 表示该层顶到了剖面的底/顶而被截断，
// 真实边界在剖面之外，对应的厚度只是下限。
type CloudLayer struct {
	BaseMSL  float64
	TopMSL   float64
	MaxCC    float64
	MaxRH    float64
	OpenTop  bool
	OpenBase bool
}

// Thickness 返回云厚（米），恒为非负。
func (c CloudLayer) Thickness() float64 {
	if d := c.TopMSL - c.BaseMSL; d > 0 {
		return d
	}
	return 0.0
}

// RHOnly 判断该层是否只靠湿度判出来的：层内最大云量都没到阈值，
// 说明模式并不认为这里有云，结论置信度更低，评级时按「风险」而非「不宜」处理。
func (c CloudLayer) RHOnly(t config.Thresholds) bool {
	return c.MaxCC < t.CloudCoverThreshold
}
