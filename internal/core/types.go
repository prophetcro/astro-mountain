package core

import (
	"io"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/report"
)

// core 以类型别名（=）把 model 的核心数据模型再导出，
// 让 cli/menu 等上层只依赖 core 即可跑完一次完整分析，无需再直接 import model。
// 别名与原类型完全等价，可互相赋值。
type (
	// OptFloat 可空浮点：Valid 为 false 表示缺测，可区分『0』与『无数据』。
	OptFloat = model.OptFloat

	// NullString 可空字符串，用于 Relation 等可能缺失的文本字段。
	NullString = model.NullString

	// Site 观测站点及其静态元数据。
	Site = model.Site

	// Surface 地表层逐小时气象要素。
	Surface = model.Surface

	// RawLevel 单个气压层的原始观测（云量 / 位势高 / 相对湿度）。
	RawLevel = model.RawLevel

	// LayerInfo 单层云的几何与湿度信息。
	LayerInfo = model.LayerInfo

	// HourRow 单站点单小时的完整评估行，是报告渲染的基本单元。
	HourRow = model.HourRow

	// ReportMeta 报告元信息。
	ReportMeta = model.ReportMeta
)

// 评级结论常量，转发自 model。严重程度由低到高：OK < WARN < BAD < NODATA。
const (
	RATING_OK     = model.RATING_OK
	RATING_WARN   = model.RATING_WARN
	RATING_BAD    = model.RATING_BAD
	RATING_NODATA = model.RATING_NODATA

	// RATING_CLEAR 是 RATING_OK 的别名，供旧调用方兼容使用。
	RATING_CLEAR = model.RATING_CLEAR
)

// 云层与机位的相对关系标签，转发自 model。
const (
	REL_CLEAR     = model.REL_CLEAR
	REL_SEA_BELOW = model.REL_SEA_BELOW
	REL_IN_CLOUD  = model.REL_IN_CLOUD
	REL_OVERHEAD  = model.REL_OVERHEAD
	REL_NODATA    = model.REL_NODATA
)

// 从 model 转发的构造与比较辅助：Num 构造有效值，Missing 构造缺测值，
// Worse 取两个评级中更严重的一个。
var (
	Num     = model.Num
	Missing = model.Missing
	Worse   = model.Worse
)

// RunParams 是一次分析运行的全部输入，由 CLI flag 或交互菜单填充。
//
// 观测夜区间二选一：给 Peak（配合 Days，表示极大日及其之前 Days 天）
// 或同时给 Start/End；两者皆缺时 ResolveRange 返回参数错误。
type RunParams struct {
	Peak  string // 流星雨极大日，YYYY-MM-DD
	Days  int    // 极大日之前额外纳入的夜数，与 Peak 搭配
	Start string // 起始日，YYYY-MM-DD，与 End 搭配
	End   string // 结束日，YYYY-MM-DD；作为夜的边界是开区间

	// Mode 运行模式：空或 "meteor" 为流星雨（默认）；"sunrise" 为日出云海模式。
	// 日出模式下用 SunriseDate 指定日出当天日期，覆盖 Peak/Start/End 的语义。
	Mode string
	// SunriseDate 日出模式：所选日出当天日期 YYYY-MM-DD（如 2026-12-14）。
	// 分析的是其前一夜（含该日日出时分）。
	SunriseDate string

	Source    Source // 数据源：A 轨 Open-Meteo 或 B 轨 Tomorrow.io
	Models    string // 显式指定的预报模式；为空时按站点 Region 自动解析
	Compare   bool   // 强制开启双模型交叉对比（与 config.api.cross_model 取或）
	NoCompare bool   // 强制关闭双模型交叉对比（覆盖 config.api.cross_model）
	SitesPath string // 点位配置文件路径；Sites 非空时忽略
	Sites     []Site // 直接注入的点位，优先于 SitesPath，便于测试与菜单临时点位
	NoCache   bool   // 禁用响应缓存，强制回源取数

	OutDir     string // 输出目录；为空时取配置，仍为空则回落到 "reports"
	ExportCSV  bool
	ExportJSON bool
	NoReport   bool // 跳过 Markdown 报告生成
	Douyin     bool // 额外渲染抖音竖图

	Stdout  io.Writer // 终端报告输出目标；为空时回落到 Engine.Stdout，再回落到 os.Stdout
	Quiet   bool      // 不打印终端报告，只产出文件
	Verbose bool
}

// ExecResult 是一次运行的全部产出：数据、生成的文件路径与诊断信息。
//
// ExitCode 约定：0 成功；1 运行期失败（无有效数据、导出失败、执行被取消）；
// 2 参数或配置错误。Warnings 是不致命的提示，不影响退出码。
type ExecResult struct {
	Rows   []HourRow // A 轨逐小时评估行，已按时间、站点稳定排序
	Meta   ReportMeta
	Nights []string // Rows 中实际出现的观测夜 ID，升序去重
	Sites  []Site   // 本次参与分析的启用点位

	Tomorrow []*dualtrack.TrackResult // B 轨逐点位装配结果，仅走 Tomorrow.io 时非空

	// Compare 是双模型交叉对比的逐小时配对结果（ICON vs GFS），独立于 Rows，
	// 不进入默认轨 CSV/JSON 序列化；仅在对比模式开启且第二模型取数成功时非空。
	Compare []model.ModelCompareRow

	ReportPath string
	CSVPath    string
	JSONPath   string
	ImagePaths []string

	// Sunrise 是「日出云海模式」下各站点的聚合结果；流星雨模式为空。
	// 与 Rows 互斥：日出模式不走逐小时 HourRow 管线，单独渲染日出报告。
	Sunrise []report.SunriseSiteResult

	Warnings []string
	Errors   []string
	ExitCode int
}

// HasData 报告结果中是否至少有一行拿到了有效气象数据。
func (r ExecResult) HasData() bool {
	for i := range r.Rows {
		if r.Rows[i].HasData {
			return true
		}
	}
	return false
}

// ImageRenderer 把 Markdown 报告渲染成图片（抖音竖图）。
//
// core 只依赖这个接口，具体实现由上层组装时注入，避免内核反向依赖渲染层。
type ImageRenderer interface {
	// Name 返回渲染器名称，用于日志与失败提示。
	Name() string

	// Render 读取 mdPath 指向的 Markdown 报告，向 outDir 写出图片，
	// 返回生成的图片路径列表。
	Render(mdPath string, cfg config.Config, outDir string) ([]string, error)
}
