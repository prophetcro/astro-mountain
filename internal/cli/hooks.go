package cli

import (
	"context"
	"io"
	"log/slog"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
)

func newStderrLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {

			if a.Key == slog.TimeKey && len(groups) == 0 {
				return slog.Attr{}
			}
			return a
		},
	}))
}

var MenuRunner func(ctx context.Context, cfg config.Config,
	configPath, sitesPath string, engine *core.Engine) int

func MenuAvailable() bool { return MenuRunner != nil }

var TomorrowFetcherFactory func(cfg config.Config) core.TomorrowFetcher

func buildTomorrowFetcher(cfg config.Config) core.TomorrowFetcher {
	if TomorrowFetcherFactory == nil {
		return nil
	}
	return TomorrowFetcherFactory(cfg)
}

// MeteoblueFetcherFactory 由组合根（cmd/astro-mountain/main.go）注入，
// 把 vendor 包 api/meteoblue 的客户端接入 cli；为 nil 表示 C 轨未接线。
// 注入点留在 main，core/cli 自身不依赖 api/meteoblue（vendor 隔离红线）。
var MeteoblueFetcherFactory func(cfg config.Config) core.MeteoblueFetcher

func buildMeteoblueFetcher(cfg config.Config) core.MeteoblueFetcher {
	if MeteoblueFetcherFactory == nil {
		return nil
	}
	return MeteoblueFetcherFactory(cfg)
}
