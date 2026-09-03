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
//     淹没型即高山云海典型形态：云从山脚一路堆过机位、脚下没有独立层，
//     机位处在云层顶部附近，可守候云隙破云，但能见度与稳定性都差。
//   - Kind：形态标签 profile.SEA_BELOW / profile.SEA_SUBMERGED。
//   - PeakThickness：时段内云层最厚时的厚度（米）。
//   - HoursCount：构成该时段的整点数（只计实测有云海的时次）。
//   - MissingHours：夹在该时段中间、廓线缺测的时次数。
//     不计入 HoursCount（缺测不等于有云海），但也不切断时段
//     （缺测同样不等于云海散了），由渲染层如实标注。
//
// 该类型原定义在 core 包；因 report 渲染日出报告需要、又不可反向依赖 core
// （core 已经 import report），故迁至 report 包，core 经 report.CloudSeaEpisode 引用。
type CloudSeaEpisode struct {
	Start         time.Time
	End           time.Time
	TopMSL        float64
	TopAGL        float64
	Submerged     bool
	Kind          string
	PeakThickness float64
	HoursCount    int
	MissingHours  int
}

// SunriseSiteResult 单站点「日出云海模式」的聚合结果。
//
// 由 core.BuildSunriseReport 填充，report 负责渲染（Markdown + 终端）。
// 字段覆盖用户关心的四件事：云海出现/消散时间、云海距机位高度、朝霞强度、
// 建议抵达时间，以及云海可信度（五档：极高/高/中/低/极低，绝不伪造百分比）
// 与一句话结论（Rating）。云海形态（脚下型/淹没型）单独成字段，便于报告与汇总表直接展示。
type SunriseSiteResult struct {
	Site          string
	SunriseTime   time.Time
	ArriveBy      time.Time
	Episodes      []CloudSeaEpisode
	CloudSeaHours int
	HasData       bool

	CloudSeaForm string // 云海形态：脚下型（云顶在机位下方）/ 淹没型（机位埋在云顶附近）

	DawnGlow     string // 朝霞四档：无 / 小烧 / 中烧 / 大烧
	DawnGlowNote string

	Confidence     string // 云海可信度（五档）：极高 / 高 / 中 / 低 / 极低
	ConfidenceNote string

	Rating string // 一句话结论（✅/⚠️/🔴 前缀）
}
