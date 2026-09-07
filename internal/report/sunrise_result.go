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

// FogPeriod 是日出夜间的一段「近地辐射雾」连续时段，与云海判定完全独立。
//
// 刻意与 CloudSeaEpisode 平级、渲染时分成两行（**云海时段** 与 **辐射雾时段**），
// 绝不合并或相互污染——这正是抖音把「山脚辐射雾」混称「云瀑」的含糊陷阱，
// 报告必须一眼能分辨「脚下有连续云海」与「贴地有辐射雾」是两件不同的事。
//   - Start/End：该时段首尾整点的本地时刻（End 为消散时刻，含至 End 前一小时）。
//   - PeakLevel：时段内峰值档位（强/中）。
//   - PeakHour：峰值出现的整点。
type FogPeriod struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	PeakLevel string    `json:"peak_level"`
	PeakHour  time.Time `json:"peak_hour"`
}

// GlowWindow 朝霞「可持续窗口」：从云顶刚被晨光扫到、到太阳太高霞色发白之间的可守候时段。
//
// 起因是用户实地反馈「朝霞转瞬即逝」：报告原先只给朝霞档位（无/小烧/中烧/大烧），
// 不告诉用户这档霞几点到几点在、能持续多久，实测霞就那几分钟，很容易错过。
// 判据是纯几何的：云顶比地面更早见到太阳，提前量即地平俯角 dip，
// 窗口起点 = 太阳高度角达到 −dip(云顶相对机位高度) 的时刻，
// 窗口终点 = 太阳高度角达到消退角（8°）的时刻。由 core.ComputeGlowWindow 计算填充。
//
// 类型刻意定义在 report 包：core 已 import report，report 不可反向依赖 core，
// 故由 core 侧以别名（core.GlowWindow）引用并负责填充。
type GlowWindow struct {
	Start       time.Time `json:"start,omitempty"`
	End         time.Time `json:"end,omitempty"`
	DurationMin int       `json:"duration_min"`
	Lit         bool      `json:"lit"`    // 是否算出有效窗口（有云载体且几何上可被晨光照到）
	Reason      string    `json:"reason"` // 无窗口时的原因（如「无云载体」）；有窗口时为判据说明
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

	// SunriseDate 该结果对应的「日出当天」YYYY-MM-DD，多日模式用于把报告按日期分节。
	// 单日模式也填，渲染层优先用它（缺省时回落 SunriseTime 的日期）。
	SunriseDate string

	CloudSeaForm string // 云海形态：脚下型（云顶在机位下方）/ 淹没型（机位埋在云顶附近）

	DawnGlow     string // 朝霞四档：无 / 小烧 / 中烧 / 大烧
	DawnGlowNote string

	// GlowWindow 朝霞可持续窗口（起止时刻 + 分钟数），由 core.ComputeGlowWindow 填充。
	// Lit=false 表示无有效窗口（无云载体），渲染层据此输出「无（无可染红云载体）」；
	// 窗口偏短（<15min）时渲染层追加「转瞬即逝」提示，提醒提前到位守候。
	GlowWindow GlowWindow `json:"glow_window,omitempty"`

	// DawnGlowModels 朝霞「逐模型判定明细」（含主模型），用于分歧透明化：
	// 把每个模型各自算出的朝霞档位摊开，决策权交还用户，而非只看共识封顶后的结论。
	// 仅共识模式（多模型）下填充；单模型 / --no-cross-model 下为空。
	DawnGlowModels []DawnGlowModelVerdict `json:"dawn_glow_models,omitempty"`
	// DawnGlowDivergence 分歧一句话描述，如「模型一致：无」或「模型分歧：仅 1/4 判大烧」。
	DawnGlowDivergence string `json:"dawn_glow_divergence,omitempty"`

	// FogPotential 近地体积雾（辐射雾）可能档位：强 / 中 / 弱 / 无。
	// 取值来自 profile.FOG_*，是**正面信号**——对云海/朝霞摄影来说贴地雾本身
	// 就是拍摄主体（不是观星模式里那个「起雾=不宜」的否决项），
	// 因此在「无云海 + 大烧朝霞」时也要如实给出，让用户知道现场有没有地面雾可拍。
	// 「无」由渲染层跳过该行（与 CloudSeaForm 空值跳过的处理一致）。
	FogPotential string
	// FogNote 近地雾判定的理由：证据串（地面RH / 温露差 / 风速 / 能见度）
	// + 辐射雾的加成或抑制说明；能见度缺测时明写「按近地 RH 代理判定」。
	FogNote string

	// FogPeriods 日出夜间的「辐射雾时段」列表（独立于云海判定）。
	// 由 assessDawnGroundFogPeriods 填充：逐时 AssessGroundFog、把雾档≥中(可拍)的
	// 连续小时聚合成时段，并记录峰值档位/时刻。空切片表示窗口内无成片辐射雾。
	// 渲染时与 CloudSeaForm/云海时段分开成行，避免与「云海」混淆。
	FogPeriods []FogPeriod `json:"fog_periods,omitempty"`

	// ObscuredWarning 日出拍摄窗口被云雾「100% 覆盖」时的警告（空串=未触发）。
	// 覆盖两类：近地辐射雾时段、淹没型云海时段——机位处被云雾包裹，
	// 日出那刻极可能什么都看不见，需提前有心理准备并盯住云隙散开的瞬间。
	// 由 core.sunriseObscuredWarning 计算填充。
	ObscuredWarning string `json:"obscured_warning,omitempty"`

	Confidence     string // 云海可信度（五档）：极高 / 高 / 中 / 低 / 极低
	ConfidenceNote string

	Rating string // 一句话结论（✅/⚠️/🔴 前缀）
}

// DawnGlowModelVerdict 单模型对朝霞的判定，透明化展示用。
// Model 为模型标识（如 icon_seamless / gfs_seamless）；Primary 标记是否主模型（默认 ICON）。
type DawnGlowModelVerdict struct {
	Model   string `json:"model"`
	Tier    string `json:"tier"`
	Primary bool   `json:"primary"`
}
