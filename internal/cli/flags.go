package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
)

const DateLayout = "2006-01-02"

var businessFlags = map[string]bool{
	"peak":         true,
	"days":         true,
	"start":        true,
	"end":          true,
	"mode":         true,
	"sunrise-date": true,
	"models":       true,
	"source":       true,
	"sites":        true,
	"out-dir":      true,
	"csv":          true,
	"json":         true,
	"no-report":    true,
	"no-cache":     true,
	"douyin":       true,
	"no-douyin":    true,
}

type Options struct {
	Peak  string
	Days  int
	Start string
	End   string

	// Mode 运行模式：空或 "meteor" 为流星雨（默认）；"sunrise" 为日出云海模式。
	Mode string
	// SunriseDate 日出模式：所选日出当天日期 YYYY-MM-DD。
	SunriseDate string

	Source     string
	Models     string
	SitesPath  string
	ConfigPath string
	NoCache    bool

	Compare      bool // 强制开启双模型交叉对比（覆盖配置默认）
	NoCrossModel bool // 强制关闭双模型交叉对比（覆盖配置默认）

	OutDir     string
	ExportCSV  bool
	ExportJSON bool
	NoReport   bool
	Douyin     bool
	NoDouyin   bool

	Menu        bool
	NoMenu      bool
	Quiet       bool
	Verbose     bool
	ShowVersion bool
	ShowHelp    bool

	DaysSet bool

	HasBusinessFlag bool

	Rest []string

	Now func() time.Time
}

func newFlagSet(o *Options) *flag.FlagSet {
	fs := flag.NewFlagSet("astro-mountain", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	fs.StringVar(&o.Peak, "peak", "", "流星雨极大日 YYYY-MM-DD")
	fs.IntVar(&o.Days, "days", -1, "配合 --peak：额外向前包含 N 天")
	fs.StringVar(&o.Start, "start", "", "起始日期 YYYY-MM-DD")
	fs.StringVar(&o.End, "end", "", "结束日期 YYYY-MM-DD")

	fs.StringVar(&o.Mode, "mode", "", "运行模式 meteor|sunrise（默认 meteor 流星雨模式）")
	fs.StringVar(&o.SunriseDate, "sunrise-date", "", "日出云海模式：日出当天日期 YYYY-MM-DD（配合 --mode sunrise）")

	fs.StringVar(&o.Source, "source", "",
		"数据源 openmeteo|tomorrow|meteoblue（默认 openmeteo）")
	fs.StringVar(&o.Models, "models", "", "Open-Meteo 数值模式")
	fs.StringVar(&o.SitesPath, "sites", "", "点位 JSON 文件路径")
	fs.StringVar(&o.ConfigPath, "config", "", "运行参数 JSON 文件路径")
	fs.BoolVar(&o.NoCache, "no-cache", false, "禁用磁盘缓存")
	fs.BoolVar(&o.Compare, "compare", false, "强制开启双模型交叉对比（ICON ↔ GFS），覆盖配置默认")
	fs.BoolVar(&o.NoCrossModel, "no-cross-model", false, "强制关闭双模型交叉对比，仅用主模式单模型")

	fs.StringVar(&o.OutDir, "out-dir", "", "产物输出目录")
	fs.BoolVar(&o.ExportCSV, "csv", false, "导出 CSV")
	fs.BoolVar(&o.ExportJSON, "json", false, "导出 JSON")
	fs.BoolVar(&o.NoReport, "no-report", false, "跳过 Markdown 报告")
	fs.BoolVar(&o.Douyin, "douyin", false, "强制生成抖音竖图")
	fs.BoolVar(&o.NoDouyin, "no-douyin", false, "强制跳过抖音竖图")

	fs.BoolVar(&o.Menu, "menu", false, "强制进入交互菜单")
	fs.BoolVar(&o.NoMenu, "no-menu", false, "强制不进入交互菜单")
	fs.BoolVar(&o.Quiet, "quiet", false, "不打印终端报表")
	fs.BoolVar(&o.Verbose, "verbose", false, "打印调试日志")
	fs.BoolVar(&o.ShowVersion, "version", false, "打印版本号后退出")
	fs.BoolVar(&o.ShowHelp, "help", false, "显示帮助")
	fs.BoolVar(&o.ShowHelp, "h", false, "显示帮助（--help 的简写）")

	return fs
}

func Parse(args []string) (*Options, error) {
	o := &Options{Days: -1}
	fs := newFlagSet(o)

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			o.ShowHelp = true
			return o, nil
		}
		return nil, fmt.Errorf("%v（用 --help 查看全部可用选项）", err)
	}

	fs.Visit(func(f *flag.Flag) {
		if f.Name == "days" {
			o.DaysSet = true
		}
		if businessFlags[f.Name] {
			o.HasBusinessFlag = true
		}
	})
	o.Rest = fs.Args()
	return o, nil
}

func (o *Options) Validate() error {
	if len(o.Rest) > 0 {
		return fmt.Errorf("不认识的位置参数：%s。本工具只接受 --xxx 形式的选项",
			strings.Join(o.Rest, " "))
	}

	if o.Douyin && o.NoDouyin {
		return fmt.Errorf("--douyin 与 --no-douyin 不能同时使用：" +
			"前者强制出图、后者强制跳过，请只保留一个")
	}
	if o.Menu && o.NoMenu {
		return fmt.Errorf("--menu 与 --no-menu 不能同时使用：" +
			"前者强制进交互菜单、后者强制直接执行，请只保留一个")
	}
	if o.Mode == "sunrise" {
		// 日出模式独占：以「日出当天」为锚，忽略 peak/start/end，不与它们混用。
		if o.SunriseDate == "" {
			return fmt.Errorf("--mode sunrise 必须配合 --sunrise-date（日出当天 YYYY-MM-DD）")
		}
		if _, err := parseDate("--sunrise-date", o.SunriseDate); err != nil {
			return err
		}
	} else {
		if o.Peak != "" && (o.Start != "" || o.End != "") {
			return fmt.Errorf("--peak 与 --start/--end 不能同时使用：" +
				"前者按极大日往前推算观测夜，后者是显式区间，请只保留一种方式")
		}

		if o.Peak != "" {
			if _, err := parseDate("--peak", o.Peak); err != nil {
				return err
			}
		}
		var startT, endT time.Time
		var err error
		if o.Start != "" {
			if startT, err = parseDate("--start", o.Start); err != nil {
				return err
			}
		}
		if o.End != "" {
			if endT, err = parseDate("--end", o.End); err != nil {
				return err
			}
		}

		if (o.Start == "") != (o.End == "") {
			return fmt.Errorf("--start 与 --end 必须成对出现：" +
				"只给一端无法确定区间，若只想指定极大日请改用 --peak")
		}
		if o.Start != "" && o.End != "" && endT.Before(startT) {
			return fmt.Errorf("--end（%s）不能早于 --start（%s）", o.End, o.Start)
		}

		if o.DaysSet && o.Days < 1 {
			return fmt.Errorf("--days 必须 ≥ 1，当前为 %d。"+
				"该参数表示在极大日基础上额外向前包含的天数", o.Days)
		}
		if o.DaysSet && o.Peak == "" {
			return fmt.Errorf("--days 只在配合 --peak 时有意义，" +
				"显式区间请直接用 --start/--end 控制长度")
		}
	}

	if _, err := core.ParseSource(o.Source); err != nil {
		return fmt.Errorf("--source %v", err)
	}

	return nil
}

func parseDate(flagName, value string) (time.Time, error) {
	t, err := time.ParseInLocation(DateLayout, value, time.UTC)
	if err != nil || t.Format(DateLayout) != value {
		return time.Time{}, fmt.Errorf(
			"%s 日期格式错误：%q 不是合法的 YYYY-MM-DD（例如 2026-08-12）", flagName, value)
	}
	return t, nil
}

func (o *Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *Options) ResolveDays(cfg config.Config) int {
	if o.DaysSet && o.Days >= 0 {
		return o.Days
	}
	if cfg.Output.DefaultDays > 0 {
		return cfg.Output.DefaultDays
	}
	return 0
}

func (o *Options) ResolveSource() core.Source {
	src, err := core.ParseSource(o.Source)
	if err != nil {
		return core.DefaultSource
	}
	return src
}

func (o *Options) ResolveDouyin(cfg config.Config) bool {
	switch {
	case o.NoDouyin:
		return false
	case o.Douyin:
		return true
	default:
		return cfg.Output.AutoDouyin
	}
}

func (o *Options) ShouldEnterMenu(stdinIsTTY bool) bool {
	switch {
	case o.NoMenu:
		return false
	case o.Menu:
		return true
	case o.HasBusinessFlag:
		return false
	default:
		return stdinIsTTY
	}
}

func (o *Options) IsImplicitBatch(stdinIsTTY bool) bool {
	if o.HasBusinessFlag || o.Menu || o.NoMenu {
		return false
	}
	return !o.ShouldEnterMenu(stdinIsTTY)
}

func (o *Options) BuildRunParams(cfg config.Config, stdout io.Writer) core.RunParams {
	p := core.RunParams{
		Peak:        o.Peak,
		Days:        o.ResolveDays(cfg),
		Start:       o.Start,
		End:         o.End,
		Mode:        o.Mode,
		SunriseDate: o.SunriseDate,
		Source:      o.ResolveSource(),
		Models:      o.Models,
		Compare:     o.Compare,
		NoCompare:   o.NoCrossModel,
		SitesPath:   o.SitesPath,
		NoCache:     o.NoCache,
		OutDir:      o.OutDir,
		ExportCSV:   o.ExportCSV,
		ExportJSON:  o.ExportJSON,
		NoReport:    o.NoReport,
		Douyin:      o.ResolveDouyin(cfg),
		Stdout:      stdout,
		Quiet:       o.Quiet,
		Verbose:     o.Verbose,
	}
	if p.Mode == "sunrise" {
		// 日出模式由 SunriseDate 锚定，不需要也不能回填 start/end 默认值。
		//
		// 抖音竖图按流星雨报告的章节名匹配，日出报告章节不同、渲染不出图。
		// 这里只认用户显式给的 --douyin，不继承配置里的 auto_douyin，
		// 免得每次日出运行都弹一条「不支持出图」的警告刷屏。
		p.Douyin = o.Douyin
		return p
	}
	if p.Peak == "" && p.Start == "" && p.End == "" {
		days := o.ResolveDays(cfg)
		if days < 1 {
			days = 1
		}
		today := o.now().UTC()
		today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		p.Start = today.Format(DateLayout)
		p.End = today.AddDate(0, 0, days).Format(DateLayout)
	}
	return p
}
