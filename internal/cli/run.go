package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
	"github.com/prophetcro/astro-mountain/internal/render"
)

var _ core.ImageRenderer = (*render.Renderer)(nil)

const (
	ExitOK      = 0
	ExitRuntime = 1
	ExitUsage   = 2
)

func Main(args []string, version string) int {
	return MainWith(args, version, os.Stdout, os.Stderr, StdinIsTTY())
}

func MainWith(args []string, version string, stdout, stderr io.Writer, stdinIsTTY bool) int {

	opts, err := Parse(args)
	if err != nil {
		fmt.Fprintln(stderr, "参数错误："+err.Error())
		return ExitUsage
	}

	if opts.ShowHelp {
		PrintHelp(stdout, version)
		return ExitOK
	}
	if opts.ShowVersion {
		fmt.Fprintln(stdout, "astro-mountain "+version)
		return ExitOK
	}

	if err := opts.Validate(); err != nil {
		fmt.Fprintln(stderr, "参数错误："+err.Error())
		fmt.Fprintln(stderr, "用 astro-mountain --help 查看完整用法。")
		return ExitUsage
	}

	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		fmt.Fprintln(stderr, "配置错误："+err.Error())
		return ExitUsage
	}
	if opts.Verbose {
		fmt.Fprintln(stderr, "[debug] 配置来源："+cfg.Source)
	}

	engine := buildEngine(cfg, opts, stdout, stderr)

	if reason := core.TomorrowUnavailableReason(
		opts.ResolveSource(), cfg, engine.TomorrowDeliverable()); reason != "" {
		fmt.Fprintln(stderr, "参数错误：--source tomorrow 无法生效——"+reason)
		fmt.Fprintln(stderr,
			"已中止，未生成任何报告：不会用 Open-Meteo（A 轨）替你出一份你没要的报告。")
		return ExitUsage
	}

	if reason := core.MeteoblueUnavailableReason(
		opts.ResolveSource(), cfg, engine.MeteoblueDeliverable()); reason != "" {
		fmt.Fprintln(stderr, "参数错误：--source meteoblue 无法生效——"+reason)
		fmt.Fprintln(stderr,
			"已中止，未生成任何报告：不会用 Open-Meteo（A 轨）替你出一份你没要的报告。")
		return ExitUsage
	}

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.ShouldEnterMenu(stdinIsTTY) {
		if MenuRunner != nil {
			return MenuRunner(ctx, cfg, opts.ConfigPath, opts.SitesPath, engine)
		}
		fmt.Fprintln(stderr, "提示：交互菜单尚未接入，已改为执行默认任务。")
	} else if opts.IsImplicitBatch(stdinIsTTY) {
		fmt.Fprintln(stderr,
			"提示：检测到非交互环境（stdin 不是终端），跳过菜单直接执行默认任务；"+
				"如需菜单请加 --menu。")
	}

	params := opts.BuildRunParams(cfg, stdout)
	res := engine.Run(ctx, params)
	printResult(stderr, res)
	return res.ExitCode
}

func buildEngine(cfg config.Config, opts *Options, stdout, stderr io.Writer) *core.Engine {
	engine := core.NewEngine(cfg)
	engine.Stdout = stdout

	engine.TomorrowFetcher = buildTomorrowFetcher(cfg)
	engine.MeteoblueFetcher = buildMeteoblueFetcher(cfg)

	renderer := render.New(cfg.Douyin)
	if opts.Verbose {

		renderer.Logger = newStderrLogger(stderr)
		engine.Logf = func(format string, a ...any) {
			fmt.Fprintf(stderr, "[debug] "+format+"\n", a...)
		}
	}
	engine.Renderer = renderer
	return engine
}

func printResult(stderr io.Writer, res core.ExecResult) {
	for _, warn := range res.Warnings {
		fmt.Fprintln(stderr, "⚠️  "+warn)
	}
	for _, e := range res.Errors {
		fmt.Fprintln(stderr, "❌ "+e)
	}
	if res.ReportPath != "" {
		fmt.Fprintln(stderr, "📄 Markdown 报告："+res.ReportPath)
	}
	if res.CSVPath != "" {
		fmt.Fprintln(stderr, "📄 CSV："+res.CSVPath)
	}
	if res.JSONPath != "" {
		fmt.Fprintln(stderr, "📄 JSON："+res.JSONPath)
	}
	for _, p := range res.ImagePaths {
		fmt.Fprintln(stderr, "🖼️  抖音竖图："+p)
	}
}

func StdinIsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
