package main

import (
	"context"
	"os"

	"github.com/prophetcro/astro-mountain/internal/api/meteoblue"
	"github.com/prophetcro/astro-mountain/internal/api/tomorrow"
	"github.com/prophetcro/astro-mountain/internal/cli"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
	"github.com/prophetcro/astro-mountain/internal/menu"
)

var Version = "dev"

var BuildTime = "unknown"

func versionString() string {
	if BuildTime == "" || BuildTime == "unknown" {
		return Version
	}
	return Version + " (built " + BuildTime + ")"
}

func main() {

	cli.MenuRunner = func(ctx context.Context, cfg config.Config,
		configPath, sitesPath string, engine *core.Engine) int {
		return menu.Run(ctx, menu.Options{
			Cfg:        cfg,
			ConfigPath: configPath,
			SitesPath:  sitesPath,
			Engine:     engine,
			Version:    Version,
		})
	}

	cli.TomorrowFetcherFactory = func(cfg config.Config) core.TomorrowFetcher {
		return tomorrow.NewFetcher(cfg.API, cfg.API.CacheEnabled)
	}

	cli.MeteoblueFetcherFactory = func(cfg config.Config) core.MeteoblueFetcher {
		return meteoblue.New(cfg)
	}

	os.Exit(cli.Main(os.Args[1:], versionString()))
}
