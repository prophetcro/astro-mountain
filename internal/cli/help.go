package cli

import (
	"fmt"
	"io"
)

const helpTemplate = `astro-mountain %s — 山地星空 / 流星雨低云海拔评估工具

用法：
  astro-mountain [选项]

不带任何业务选项且在终端中运行时，进入交互菜单；在管道 / CI 等非终端环境中，
则直接执行默认任务（自今夜起 output.default_days 个观测夜）。

运行模式：
  --mode   NAME         meteor|sunrise，默认 meteor
                        meteor   流星雨模式：逐夜逐时评级，看头顶通透与辐射点高度
                        sunrise  日出云海模式：只看一个清晨，输出云海出现/消散时间、
                                 云海距机位高度、朝霞强度（无/小烧/中烧/大烧）、
                                 建议抵达机位时间；不产出逐小时 CSV/JSON，
                                 也不支持抖音竖图（竖图按流星雨报告章节名匹配）
  --sunrise-date YYYY-MM-DD[,YYYY-MM-DD...]
                        配合 --mode sunrise：日出当天日期（不是前一夜）。
                        工具自动取该日【前一夜】的预报并覆盖到日出之后。
                        支持逗号分隔多个日期一次查多日，如
                        --sunrise-date 2026-08-14,2026-08-15,2026-08-16
                        （最多 16 个，与 Open-Meteo 16 天预报窗口对齐）

时间范围（流星雨模式，--peak 与 --start/--end 二选一，不可混用）：
  --peak   YYYY-MM-DD   流星雨极大日
  --days   N            配合 --peak：在极大日基础上额外向前包含 N 天（N ≥ 1）
                        未指定时取配置 output.default_days
  --start  YYYY-MM-DD   起始日期（必须与 --end 成对出现）
  --end    YYYY-MM-DD   结束日期（不得早于 --start）

数据源：
  --source NAME         用哪个气象源来算，openmeteo|tomorrow|meteoblue，默认 openmeteo
                        openmeteo  多模式气压层廓线，可判"脚下云海"，无配额限制；
                                   孤立高峰（模式地形被抹平的山头）首选
                        tomorrow   直接给出云底高度，适合开阔平原；无云顶字段，
                                   云底低于机位时无法区分云海与身处云中
                                   免费配额 500 次/天、25 次/小时、3 次/秒
                        meteoblue  山地高分辨率融合预报（分层云量/降水/能见度）；
                                   不反演云海几何（无气压层），需 API key
  --models NAMES        Open-Meteo 数值模式，默认取配置 api.models（icon_seamless）
  --sites  PATH         点位 JSON 路径，默认按 ./configs/sites.json 优先级查找
  --config PATH         运行参数 JSON 路径，默认按 ./configs/config.json 优先级查找
  --no-cache            禁用磁盘缓存，强制重新请求

输出：
  --out-dir PATH        产物输出目录，默认取配置 output.out_dir
  --csv                 额外导出 CSV
  --json                额外导出 JSON
  --no-report           跳过 Markdown 报告
  --douyin              强制生成抖音竖图
  --no-douyin           强制跳过抖音竖图
                        两者都不给时取配置 output.auto_douyin（发布默认开启，
                        即每次生成报告都会自动出图）

运行方式：
  --menu                强制进入交互菜单
  --no-menu             强制不进入交互菜单，直接执行
  --quiet               不打印终端报表（仍产出文件）
  --verbose             打印调试日志到 stderr
  --version             打印版本号后退出
  --help, -h            显示本帮助

退出码：
  0  成功
  1  运行期失败（无有效数据 / 导出失败）
  2  参数或配置错误

示例：
  # 1. 评估 2026 年英仙座流星雨极大及之前 5 天
  astro-mountain --peak 2026-08-12 --days 5

  # 2. 评估显式日期区间，并额外导出 CSV 与 JSON
  astro-mountain --start 2026-08-10 --end 2026-08-15 --csv --json

  # 3. 只出抖音竖图，不刷屏终端
  astro-mountain --peak 2026-08-12 --quiet --douyin

  # 4. 用自定义点位文件，本次不出图、不读缓存
  astro-mountain --sites ./my_sites.json --no-douyin --no-cache

  # 5. CI / 定时任务里强制非交互执行，产物落到指定目录
  astro-mountain --no-menu --peak 2026-08-12 --out-dir ./artifacts

  # 6. 开阔平原点位，改用 Tomorrow.io 的云底数据
  astro-mountain --peak 2026-08-12 --source tomorrow

  # 7. 日出云海模式：看 8 月 14 日清晨的云海与朝霞（自动取 8/13 夜的预报）
  astro-mountain --mode sunrise --sunrise-date 2026-08-14

  # 8. 日出云海模式一次查多日：连看 8/14、8/15、8/16 三个清晨（报告按日期分节）
  astro-mountain --mode sunrise --sunrise-date 2026-08-14,2026-08-15,2026-08-16
`

func HelpText(version string) string {
	return fmt.Sprintf(helpTemplate, version)
}

func PrintHelp(w io.Writer, version string) {
	fmt.Fprint(w, HelpText(version))
}
