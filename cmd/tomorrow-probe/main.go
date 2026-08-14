package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/prophetcro/astro-mountain/internal/api/tomorrow"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

const (
	defaultLat  = 28.2269
	defaultLon  = 86.9209
	defaultAlt  = 5200.0
	defaultName = "珠峰大本营参考点"
)

func main() {
	var (
		lat  = flag.Float64("lat", defaultLat, "probe latitude")
		lon  = flag.Float64("lon", defaultLon, "probe longitude")
		alt  = flag.Float64("alt", defaultAlt, "probe altitude (m)")
		name = flag.String("name", defaultName, "probe site name")
		key  = flag.String("key", "", "api key 兜底（优先用环境变量 TOMORROW_API_KEY）")
		ep   = flag.String("endpoint", tomorrow.DefaultEndpoint, "覆盖端点")
	)
	flag.Parse()

	resolved, source, ok := tomorrow.ResolveAPIKey(os.Getenv("TOMORROW_API_KEY"), *key)
	if !ok {
		fmt.Fprintln(os.Stderr, "✗ 未找到 API key：请设置环境变量 TOMORROW_API_KEY 或通过 --key 传入")
		fmt.Fprintln(os.Stderr, "  当前走 UnitAuto 启发式降级，不发起真实请求。")
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "• 使用 API key 来源: %s\n", source)

	cfg := config.APIConfig{TomorrowEndpoint: *ep}
	c := tomorrow.New(cfg, false, tomorrow.WithAPIKey(resolved, source))
	site := model.Site{Name: *name, Lat: *lat, Lon: *lon, Alt: *alt}

	if _, err := tomorrow.Probe(context.Background(), c, site, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "✗ 探针失败: %v\n", err)
		os.Exit(1)
	}
}
