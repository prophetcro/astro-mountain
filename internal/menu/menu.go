package menu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
	"github.com/prophetcro/astro-mountain/internal/report"
)

type DouyinRenderFunc func(mdPath, outDir, sections, fontPath string) ([]string, error)

type FontProbeFunc func(override string) (string, error)

type Options struct {
	Cfg config.Config

	ConfigPath string

	SitesPath string

	Engine *core.Engine

	Stdin io.Reader

	Stdout io.Writer

	Version string

	RenderReport DouyinRenderFunc

	FontProbe FontProbeFunc

	Now func() time.Time
}

func Run(ctx context.Context, opts Options) int {
	if ctx == nil {
		ctx = context.Background()
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	s := &state{
		opts: opts,
		u:    newUI(ctx, stdin, stdout),
		ctx:  ctx,
		cfg:  opts.Cfg,
	}
	s.init()

	err := s.loop()
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errQuit), errors.Is(err, errEOF), errors.Is(err, errBack):
		return 0
	case errors.Is(err, errCanceled):
		s.u.blank()
		s.u.info("已收到中断信号，正在退出…")
		return 0
	default:
		s.u.blank()
		s.u.printf("  ✗ 菜单异常退出：%v\n", err)
		return 2
	}
}

type state struct {
	opts Options
	u    *ui
	ctx  context.Context

	cfg      config.Config
	cfgDirty bool

	sites      []config.Site
	sitesDirty bool
	sitesSrc   string
	sitesWarns []string

	configPath string
	sitesPath  string

	fontPath string
	fontErr  error

	lastReport *reportForm
}

func (s *state) init() {
	res, err := config.LoadSites(s.opts.SitesPath)
	if err != nil {
		s.sitesWarns = append(s.sitesWarns, "加载点位失败："+err.Error())
		s.sites = append([]config.Site(nil), config.DefaultSites()...)
		s.sitesSrc = config.BuiltinSource
	} else {
		s.sites = append([]config.Site(nil), res.Sites...)
		s.sitesSrc = res.Source
		s.sitesWarns = append(s.sitesWarns, res.Warnings...)
	}

	s.sitesPath = resolveWritePath(s.opts.SitesPath, s.sitesSrc, config.SitesFileName)
	s.configPath = resolveWritePath(s.opts.ConfigPath, s.cfg.Source, config.ConfigFileName)

	s.fontPath, s.fontErr = s.probeFont(s.cfg.Douyin.FontPath)
}

func resolveWritePath(explicit, source, name string) string {
	if strings.TrimSpace(explicit) != "" {
		return explicit
	}
	if source != "" && source != config.BuiltinSource {
		return source
	}
	return filepath.Join("configs", name)
}

func (s *state) probeFont(override string) (string, error) {
	probe := s.opts.FontProbe
	if probe == nil {
		probe = defaultFontProbe
	}
	return probe(override)
}

func (s *state) renderReport(mdPath, outDir, sections, fontPath string) ([]string, error) {
	fn := s.opts.RenderReport
	if fn == nil {
		fn = defaultRenderReport
	}
	return fn(mdPath, outDir, sections, fontPath)
}

func (s *state) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now()
}

func (s *state) enabledSites() []config.Site {
	out := make([]config.Site, 0, len(s.sites))
	for _, site := range s.sites {
		if site.IsEnabled() {
			out = append(out, site)
		}
	}
	return out
}

func (s *state) outDir() string {
	if d := strings.TrimSpace(s.cfg.Output.OutDir); d != "" {
		return d
	}
	return "reports"
}

func (s *state) douyinDir() string {
	if d := strings.TrimSpace(s.cfg.Output.DouyinDir); d != "" {
		return d
	}
	return filepath.Join(s.outDir(), "douyin")
}

func (s *state) loop() error {

	if len(s.sitesWarns) > 0 {
		s.u.blank()
		for _, w := range s.sitesWarns {
			s.u.warn(w)
		}
	}

	for {
		s.renderMainMenu()
		pick, err := s.u.choice("请选择", []string{"1", "2", "3", "4", "5", "0"}, "1", backReturns)
		if err != nil {
			if errors.Is(err, errBack) {

				return nil
			}
			return err
		}

		var ferr error
		switch pick {
		case "1":
			ferr = s.reportFlow()
		case "2":
			ferr = s.douyinFlow()
		case "3":
			ferr = s.siteManager()
		case "4":
			ferr = s.settingsFlow()
		case "5":
			ferr = s.helpFlow()
		case "0":
			return s.quitFlow()
		}

		switch {
		case ferr == nil, errors.Is(ferr, errBack):
			continue
		case isTerminal(ferr):
			return ferr
		default:
			s.u.blank()
			s.u.printf("  ✗ 操作失败：%v\n", ferr)
			if perr := s.u.pause(); perr != nil {
				return perr
			}
		}
	}
}

func (s *state) quitFlow() error {
	if s.sitesDirty || s.cfgDirty {
		s.u.blank()
		var pending []string
		if s.sitesDirty {
			pending = append(pending, "点位配置")
		}
		if s.cfgDirty {
			pending = append(pending, "运行参数")
		}
		s.u.warn(fmt.Sprintf("%s 有未保存的修改，退出后将丢失。",
			strings.Join(pending, "与")))
		yes, err := s.u.confirm("确定退出？", false)
		if err != nil {
			return err
		}
		if !yes {
			return nil
		}
	}
	s.u.blank()
	s.u.info("再见，祝拍摄顺利 ✨")
	return errQuit
}

func (s *state) version() string {
	if v := strings.TrimSpace(s.opts.Version); v != "" {
		return v
	}
	return "dev"
}

func (s *state) renderMainMenu() {
	total := len(s.sites)
	enabled := len(s.enabledSites())

	sitesLabel := s.sitesSrc
	if sitesLabel == "" {
		sitesLabel = config.BuiltinSource
	}
	cfgLabel := s.cfg.Source
	if cfgLabel == "" {
		cfgLabel = config.BuiltinSource
	}

	fontState := "已探测"
	if s.fontErr != nil {
		fontState = "未找到（抖音出图不可用）"
	} else if s.fontPath != "" {
		fontState = "已探测 " + filepath.Base(s.fontPath)
	}

	douyinState := "报告后自动出图 关"
	if s.cfg.Output.AutoDouyin {
		douyinState = "报告后自动出图 开"
	}

	s.u.blank()
	s.u.boxTop()
	s.u.boxLine("  山地星野 · 低云海拔评估工具   " + s.version())
	s.u.boxMid()
	s.u.boxKV("点位配置", sitesLabel+dirtyMark(s.sitesDirty),
		fmt.Sprintf("%d 个点位（%d 启用）", total, enabled))
	s.u.boxKV("运行配置", cfgLabel+dirtyMark(s.cfgDirty),
		"模式 "+s.cfg.API.Models)
	s.u.boxKV("输出目录", s.outDir(), douyinState)
	s.u.boxKV("中文字体", fontState, "")
	s.u.boxBottom()
	s.u.blank()

	entries := [][2]string{
		{"[1]  生成评估报告", "拉取气象数据 → Markdown 报告 → 抖音图"},
		{"[2]  仅生成抖音图片", "从已有报告重新渲染竖版图"},
		{"[3]  点位配置管理", "查看 / 新增 / 编辑 / 删除观测点位"},
		{"[4]  运行参数设置", "阈值 / 时间窗口 / 输出目录 / 字体"},
		{"[5]  查看帮助", "功能说明与全部 CLI 参数"},
		{"[0]  退出", ""},
	}
	for _, e := range entries {

		s.u.println(strings.TrimRight("   "+report.Pad(e[0], 22, report.AlignLeft)+e[1], " "))
	}
	s.u.blank()
}

func dirtyMark(dirty bool) string {
	if dirty {
		return " *"
	}
	return ""
}

func (s *state) helpFlow() error {
	u := s.u
	u.banner("[5] 查看帮助")

	u.blank()
	u.println(" ── 这个工具解决什么问题 ──")
	u.info("上山拍星空 / 流星雨 / 云海之前，最怕的不是「有云」，而是「云在哪个高度」。")
	u.info("同样是 80% 的低云量：云顶在你脚下就是云海大片，云底在你头顶就是白跑一趟。")
	u.info("本工具拉取 Open-Meteo 的 8 个气压层数据，反演出云底 / 云顶的「海拔高度」，")
	u.info("再和你的机位海拔一比，直接告诉你「云在山顶之上 / 之下 / 你正处在云里」。")

	u.blank()
	u.println(" ── 四级评级含义 ──")
	u.table("   ",
		[]string{"评级", "含义", "判据要点"},
		nil,
		[][]string{
			{model_RATING_OK, "头顶通透，可拍摄",
				"全层无云，或云顶低于机位海拔（云海在脚下）"},
			{model_RATING_WARN, "有风险，需现场判断",
				"头顶云量 40~70%，或轻雾（能见度 1000~5000m），或温露差 < 3℃"},
			{model_RATING_BAD, "不宜拍摄",
				"头顶云量 ≥ 70%，或机位在云中，或有雾（能见度 < 1000m）"},
			{model_RATING_NODATA, "无气象数据，无从判断",
				"该时次云量 / 湿度 / 能见度全部缺测"},
		})
	u.blank()
	u.warn("安全红线：❓无数据 ≠ 晴朗。数据缺测时工具绝不会误报为 ✅通透，")
	u.info("  请按「未知」对待，出发前务必用其它渠道复核。")

	u.blank()
	u.println(" ── 各菜单项作用 ──")
	u.table("   ",
		[]string{"菜单项", "说明"},
		nil,
		[][]string{
			{"[1] 生成评估报告", "选模式 → 选日期 → 选点位 → 选导出项 → 一次性产出报告与图片"},
			{"[2] 仅生成抖音图片", "对已有的 Markdown 报告重新出图，不再拉取气象数据"},
			{"[3] 点位配置管理", "增删改查观测点位，写回 sites.json（自动备份 .bak）"},
			{"[4] 运行参数设置", "改输出目录 / 默认天数 / 阈值 / 字体路径，写回 config.json"},
			{"[5] 查看帮助", "就是本页"},
			{"[0] 退出", "有未保存修改时会二次确认"},
		})

	u.blank()
	u.println(" ── 配置文件位置与加载优先级 ──")
	u.info("点位   " + s.sitesPath)
	u.info("参数   " + s.configPath)
	u.info("优先级 --sites/--config 显式路径 > ./configs/ > 可执行文件同级 configs/ > 内置默认")

	u.blank()
	u.println(" ── 用 CLI 参数跳过菜单（脚本 / crontab 场景）──")
	u.info("astro-mountain --peak 2026-08-13 --days 5        # 极大日前 5 天，共 6 夜")
	u.info("astro-mountain --start 2026-08-10 --end 2026-08-14 --csv")
	u.info("astro-mountain --peak 2026-08-13 --csv --json --out-dir ./out")
	u.info("astro-mountain --menu                            # 强制进入本菜单")
	u.info("astro-mountain --help                            # 全部参数说明")

	u.blank()
	u.println(" ── 全局交互约定 ──")
	u.info("提示语末尾的 (默认 xxx) 表示直接回车即采用该值")
	u.info("任意提示处输入 b / back 返回上一级，输入 q / quit 退出（会二次确认）")
	u.info("非法输入只会原地重问，连续 3 次无效则自动返回上级")

	return u.pause()
}

const (
	model_RATING_OK     = core.RATING_OK
	model_RATING_WARN   = core.RATING_WARN
	model_RATING_BAD    = core.RATING_BAD
	model_RATING_NODATA = core.RATING_NODATA
)
