package report

import "time"

// CloudSeaEpisode 某站点某夜一次「云海出现 → 消散」连续时段。
//
// 时段由若干连续整点的「有云海」判定组成（中间允许短暂缺失 ≤ 1h）。
// 字段含义：
//   - Start/End：该时段首尾整点的本地时刻。
//   - TopMSL：时段内云顶海拔的最大值（米）。
//   - TopAGL：云顶距机位高差的最大值（米，正=在脚下多少米；负=淹没机位）。
//   - Submerged：时段内是否有机位被云顶淹没（TopMSL > 机位海拔）。
//   - PeakThickness：时段内云层最厚时的厚度（米）。
//   - HoursCount：构成该时段的整点数。
//
// 该类型原定义在 core 包；因 report 渲染日出报告需要、又不可反向依赖 core
// （core 已经 import report），故迁至 report 包，core 经 report.CloudSeaEpisode 引用。
type CloudSeaEpisode struct {
	Start         time.Time
	End           time.Time
	TopMSL        float64
	TopAGL        float64
	Submerged     bool
	PeakThickness float64
	HoursCount    int
}

// SunriseSiteResult 单站点「日出云海模式」的聚合结果。
//
// 由 core.BuildSunriseReport 填充，report 负责渲染（Markdown + 终端）。
// 字段覆盖用户关心的四件事：云海出现/消散时间、云海距机位高度、朝霞强度、
// 建议抵达时间，以及诚实五档可信度（极高/高/中/低/极低，绝不伪造百分比）
// 与一句话结论（Rating）。
type SunriseSiteResult struct {
	Site          string
	SunriseTime   time.Time
	ArriveBy      time.Time
	Episodes      []CloudSeaEpisode
	CloudSeaHours int
	HasData       bool

	DawnGlow     string // 朝霞四档：无 / 小烧 / 中烧 / 大烧
	DawnGlowNote string

	Confidence     string // 诚实五档：极高 / 高 / 中 / 低 / 极低
	ConfidenceNote string

	Rating string // 一句话结论（✅/⚠️/🔴 前缀）
}
