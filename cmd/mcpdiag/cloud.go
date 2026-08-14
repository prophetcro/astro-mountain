package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

const dateLayout = "2006-01-02"

func veilTriggered(surface model.Surface, th config.Thresholds) bool {
	return surface.CloudCoverMid.GE(th.MidCloudVeilCC) ||
		surface.CloudCoverHigh.GE(th.HighCloudThinVeilCC)
}

const sepLine = "════════════════════════════════════════════════════════════════════"

type cloudFlags struct {
	Site    string
	Date    string
	Hour    int
	NoCache bool
}

func parseCloudFlags(args []string) (cloudFlags, error) {
	fs := flag.NewFlagSet("cloud", flag.ContinueOnError)
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintln(out, "用法：mcpdiag cloud [参数...]")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "打印指定站点/日期每个时次的：地表分层云量、气压层剖面逐层、")
		fmt.Fprintln(out, "反演云层、站点关系分类与最终评级，用于人工核实「剖面之上有云」兜底判据。")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "参数：")
		fs.PrintDefaults()
	}

	var f cloudFlags
	fs.StringVar(&f.Site, "site", "牵牛岗", "站点名（需与 sites.json 中的 name 完全一致）")
	fs.StringVar(&f.Date, "date", "2026-08-12", "日期 YYYY-MM-DD")
	fs.IntVar(&f.Hour, "hour", -1, "只打印该小时(0-23)；不指定则打印整天夜间窗口所有时次")
	fs.BoolVar(&f.NoCache, "no-cache", false, "禁用磁盘缓存，强制走网络取数")

	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if f.Hour != -1 && (f.Hour < 0 || f.Hour > 23) {
		return f, fmt.Errorf("--hour 取值 %d 超出 0-23", f.Hour)
	}
	return f, nil
}

func findSite(sites []config.Site, name string) (config.Site, error) {
	for _, s := range sites {
		if s.Name == name {
			return s, nil
		}
	}
	available := make([]string, 0, len(sites))
	for _, s := range sites {
		available = append(available, s.Name)
	}
	return config.Site{}, fmt.Errorf("未找到站点 %q；可用站点：%s",
		name, strings.Join(available, "、"))
}

func inNightWindow(h int, w config.WindowConfig) bool {
	start, end := w.NightStartHour, w.NightEndHour
	if start <= end {
		return h >= start && h <= end
	}
	return h >= start || h <= end
}

func optStr(v model.OptFloat, digits int, unit string) string {
	if !v.Valid {
		return "NaN/缺测"
	}
	return model.FormatFixed(v.V, digits) + unit
}

func posLabel(height, siteAlt float64) string {
	if height > siteAlt {
		return "[高于机位]"
	}
	return "[机位之下]"
}

func runCloud(args []string) error {
	f, err := parseCloudFlags(args)
	if err != nil {

		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	cfg, err := config.Load("")
	if err != nil {
		return fmt.Errorf("加载配置失败：%w", err)
	}
	sitesResult, err := config.LoadSites("")
	if err != nil {
		return fmt.Errorf("加载点位失败：%w", err)
	}
	site, err := findSite(sitesResult.Sites, f.Site)
	if err != nil {
		return err
	}

	start, err := time.Parse(dateLayout, f.Date)
	if err != nil {
		return fmt.Errorf("解析 --date %q 失败（需要 YYYY-MM-DD）：%w", f.Date, err)
	}
	end := start

	client := api.New(cfg.API, !f.NoCache)
	ctx := context.Background()

	resp, hourlyVars, err := client.FetchSite(ctx, site, start, end, cfg.API.Models)
	if err != nil {
		return fmt.Errorf("取数失败：%w", err)
	}

	printHeader(cfg, sitesResult, site, f, hourlyVars)

	printed := 0
	for idx := 0; idx < resp.Len(); idx++ {
		ts := resp.Times[idx]
		if f.Hour >= 0 {
			if ts.Hour() != f.Hour {
				continue
			}
		} else if !inNightWindow(ts.Hour(), cfg.Window) {
			continue
		}

		surface := resp.Surface(idx)
		levelValues := resp.LevelValues(idx)
		levels := profile.BuildProfile(levelValues, cfg.Thresh)
		layers := profile.DetectLayers(levels, cfg.Thresh)
		relation, keyLayer := profile.ClassifySite(site.Alt, layers)
		ev := profile.EvaluateHour(site, surface, layers, levels, cfg.Thresh)

		printHour(site, cfg, ts, surface, levelValues, levels, layers, relation, keyLayer, ev)
		printed++
	}

	fmt.Println(sepLine)
	if printed == 0 {
		if f.Hour >= 0 {
			fmt.Printf("时间轴上没有 %02d:00 这个时次（共 %d 个时次）。\n", f.Hour, resp.Len())
		} else {
			fmt.Printf("夜间窗口 %02d:00–%02d:00 内没有可打印的时次（共 %d 个时次）。\n",
				cfg.Window.NightStartHour, cfg.Window.NightEndHour, resp.Len())
		}
		return nil
	}
	fmt.Printf("共打印 %d 个时次。以上全部为原始中间量，未经任何加工。\n", printed)
	return nil
}

func printHeader(cfg config.Config, sitesResult config.SitesResult,
	site config.Site, f cloudFlags, hourlyVars []string) {

	fmt.Println(sepLine)
	fmt.Println("mcpdiag cloud — 云量判据只读诊断")
	fmt.Println(sepLine)
	fmt.Printf("配置来源      : %s\n", cfg.Source)
	fmt.Printf("点位来源      : %s\n", sitesResult.Source)
	fmt.Printf("站点          : %s  (lat=%.4f, lon=%.4f, alt=%.0fm MSL)\n",
		site.Name, site.Lat, site.Lon, site.Alt)
	fmt.Printf("日期          : %s\n", f.Date)
	if f.Hour >= 0 {
		fmt.Printf("时次筛选      : 仅 %02d:00\n", f.Hour)
	} else {
		fmt.Printf("时次筛选      : 夜间窗口 %02d:00–%02d:00（跨零点按闭区间）\n",
			cfg.Window.NightStartHour, cfg.Window.NightEndHour)
	}
	fmt.Printf("模式          : %s   缓存：%v\n", cfg.API.Models, !f.NoCache)
	fmt.Printf("请求变量数    : %d\n", len(hourlyVars))
	fmt.Printf("相关阈值      : cloud_cover_threshold=%.0f%%  mid_cloud_veil_cc=%.0f%%  "+
		"high_cloud_thin_veil_cc=%.0f%%  rh_low=%.0f%%  rh_high=%.0f%%\n",
		cfg.Thresh.CloudCoverThreshold, cfg.Thresh.MidCloudVeilCC,
		cfg.Thresh.HighCloudThinVeilCC,
		cfg.Thresh.RHThresholdLow, cfg.Thresh.RHThresholdHigh)
}

func printHour(site config.Site, cfg config.Config, ts time.Time,
	surface model.Surface, levelValues map[int]model.RawLevel,
	levels []profile.Level, layers []profile.CloudLayer,
	relation string, keyLayer *profile.CloudLayer, ev profile.Evaluation) {

	fmt.Println()
	fmt.Println(sepLine)
	nightTag := "白天"
	if inNightWindow(ts.Hour(), cfg.Window) {
		nightTag = "夜间窗口内"
	}
	fmt.Printf("【时次】%s（北京时间，%s）  机位海拔 %.0fm MSL\n",
		ts.Format("01-02 15:04"), nightTag, site.Alt)
	fmt.Println(sepLine)

	fmt.Println("① 地表产品原始字段（surface，不区分高度基准）")
	fmt.Printf("   cloud_cover_low       = %s   （0–3km，含雾；剖面覆盖这一段）\n",
		optStr(surface.CloudCoverLow, 0, "%"))
	fmt.Printf("   cloud_cover_mid       = %s   （3–8km；剖面顶 ≈3km，完全看不到）\n",
		optStr(surface.CloudCoverMid, 0, "%"))
	fmt.Printf("   cloud_cover_high      = %s   （8km 以上卷云；剖面完全看不到）\n",
		optStr(surface.CloudCoverHigh, 0, "%"))
	fmt.Printf("   relative_humidity_2m  = %s\n", optStr(surface.RelativeHumidity2m, 0, "%"))
	fmt.Printf("   wind_speed_10m        = %s\n", optStr(surface.WindSpeed10m, 1, " m/s"))
	fmt.Printf("   visibility            = %s\n", optStr(surface.Visibility, 0, " m"))
	fmt.Printf("   weather_code          = %s\n", optStr(surface.WeatherCode, 0, ""))
	fmt.Printf("   precipitation         = %s\n", optStr(surface.Precipitation, 1, " mm"))

	fmt.Println()
	fmt.Println("② 气压层剖面（BuildProfile 后，按高度升序；含机位之下的层）")
	if len(levels) == 0 {
		fmt.Println("   （廓线为空：8 层 cc/rh 全缺测，或全部落在 MinLevelHeightMSL 之下）")
	} else {
		fmt.Printf("   %-8s %-12s %-12s %-12s %-10s %s\n",
			"气压", "高度(MSL)", "云量CC", "湿度RH", "位置", "本层判云")
		for _, lv := range levels {
			cloudy := "—"
			if lv.Cloudy(cfg.Thresh) {
				cloudy = "有云"
			}
			fmt.Printf("   %-8s %-12s %-12s %-12s %-10s %s\n",
				fmt.Sprintf("%dhPa", lv.Pressure),
				fmt.Sprintf("%.0fm", lv.Height),
				optStr(lv.CC, 0, "%"),
				optStr(lv.RH, 0, "%"),
				posLabel(lv.Height, site.Alt),
				cloudy)
		}
	}
	printDroppedLevels(levelValues, levels)

	fmt.Println()
	fmt.Println("③ 反演云层（DetectLayers）")
	if len(layers) == 0 {
		fmt.Println("   （剖面内未反演出任何云层）")
	} else {
		for i, l := range layers {
			fmt.Printf("   层#%d  云底=%.0fm  云顶=%.0fm  云厚=%.0fm  最大CC=%.0f%%  最大RH=%.0f%%  openBase=%v openTop=%v\n",
				i+1, l.BaseMSL, l.TopMSL, l.Thickness(), l.MaxCC, l.MaxRH, l.OpenBase, l.OpenTop)
		}
	}

	fmt.Println()
	fmt.Println("④ ClassifySite 结果")
	fmt.Printf("   relation = %s\n", relation)
	if keyLayer == nil {
		fmt.Println("   keyLayer = （无决定层）")
	} else {
		fmt.Printf("   keyLayer = 云底 %.0fm / 云顶 %.0fm / 最大CC %.0f%%\n",
			keyLayer.BaseMSL, keyLayer.TopMSL, keyLayer.MaxCC)
	}

	fmt.Println()
	fmt.Println("⑤ EvaluateHour 结果")
	fmt.Printf("   rating = %s\n", ev.Rating)
	fmt.Printf("   note   = %s\n", ev.Note)

	if veilTriggered(surface, cfg.Thresh) {
		printConflict(site, cfg, surface, levels)
	}
}

func printDroppedLevels(levelValues map[int]model.RawLevel, levels []profile.Level) {
	kept := make(map[int]bool, len(levels))
	for _, lv := range levels {
		kept[lv.Pressure] = true
	}
	dropped := make([]string, 0, len(profile.PressureLevels))
	for _, p := range profile.PressureLevels {
		if kept[p] {
			continue
		}
		raw := levelValues[p]
		reason := "cc/rh 全缺测（该层无信息，非「无云」）"
		if raw.CC.Valid || raw.RH.Valid {
			reason = "高度低于 MinLevelHeightMSL（地下外推假值）或与相邻层同高被去重"
		}
		dropped = append(dropped, fmt.Sprintf("%dhPa(%s)", p, reason))
	}
	if len(dropped) > 0 {
		fmt.Printf("   剔除层：%s\n", strings.Join(dropped, "，"))
	}
}

func printConflict(site config.Site, cfg config.Config,
	surface model.Surface, levels []profile.Level) {

	ccThr := cfg.Thresh.CloudCoverThreshold
	fmt.Println()
	fmt.Println("⑥ ⚠ 判据诊断：命中「剖面(3km)之上有云」兜底")

	fmt.Println("   ── 高度口径（Open-Meteo 官方定义）──")
	fmt.Println("      cloud_cover_low  = 0–3km（含雾）")
	fmt.Println("      cloud_cover_mid  = 3–8km")
	fmt.Println("      cloud_cover_high = 8km 以上")
	fmt.Println("      气压层剖面 1000→700hPa，顶 ≈3km = mid 的起点：")
	fmt.Println("      → mid/high 在剖面里永远看不到，剖面对它们零覆盖。这是结构性盲区，")
	fmt.Println("        不是数据缺失；剖面「无云」不能用来否定下面的触发量。")

	fmt.Println("   ── 触发量（两条独立判据，分别对各自阈值）──")
	printVeilTrigger("cloud_cover_mid ", "mid_cloud_veil_cc      ", "3–8km 中云，实质遮挡",
		surface.CloudCoverMid, cfg.Thresh.MidCloudVeilCC)
	printVeilTrigger("cloud_cover_high", "high_cloud_thin_veil_cc", "8km+ 卷云，减光较轻",
		surface.CloudCoverHigh, cfg.Thresh.HighCloudThinVeilCC)

	var (
		above, below           []profile.Level
		maxCCAbove, maxRHAbove float64 = -1, -1
		maxCCBelow, maxRHBelow float64 = -1, -1
	)
	for _, lv := range levels {
		if lv.Height > site.Alt {
			above = append(above, lv)
			if lv.CC.Valid && lv.CC.V > maxCCAbove {
				maxCCAbove = lv.CC.V
			}
			if lv.RH.Valid && lv.RH.V > maxRHAbove {
				maxRHAbove = lv.RH.V
			}
			continue
		}
		below = append(below, lv)
		if lv.CC.Valid && lv.CC.V > maxCCBelow {
			maxCCBelow = lv.CC.V
		}
		if lv.RH.Valid && lv.RH.V > maxRHBelow {
			maxRHBelow = lv.RH.V
		}
	}

	fmt.Printf("   机位之上（Height > %.0fm，剖面至多到 ~3km，即只覆盖 low 的上半段）：\n", site.Alt)
	if len(above) == 0 {
		fmt.Println("      （剖面内无高于机位的层——机位已高过剖面顶，剖面对头顶完全无观测能力）")
	} else {
		for _, lv := range above {
			fmt.Printf("      %dhPa @%.0fm  CC=%s  RH=%s\n",
				lv.Pressure, lv.Height, optStr(lv.CC, 0, "%"), optStr(lv.RH, 0, "%"))
		}
	}

	fmt.Printf("   机位之下（Height < %.0fm，云海所在高度，属 low 层管辖）：\n", site.Alt)
	if len(below) == 0 {
		fmt.Println("      （剖面内无低于机位的层）")
	} else {
		for _, lv := range below {
			fmt.Printf("      %dhPa @%.0fm  CC=%s  RH=%s\n",
				lv.Pressure, lv.Height, optStr(lv.CC, 0, "%"), optStr(lv.RH, 0, "%"))
		}
	}

	midHit := surface.CloudCoverMid.GE(cfg.Thresh.MidCloudVeilCC)
	highHit := surface.CloudCoverHigh.GE(cfg.Thresh.HighCloudThinVeilCC)

	fmt.Println("   ── 判读提示 ──")
	switch {
	case midHit && highHit:
		fmt.Println("   3–8km 中云与 8km 以上高云**同时**过阈：上下两层都被盖住，")
		fmt.Println("   星野基本没有拍摄价值，不必再纠结剖面怎么说。")
	case midHit:
		fmt.Println("   触发者是 3–8km 的中云（cloud_cover_mid），不是薄卷云。")
		fmt.Println("   中云是成片实质遮挡，降级为 ⚠️风险已属保守；剖面在这个高度上没有")
		fmt.Println("   任何观测能力，无法证实也无法证伪，请以卫星云图/实况为准。")
	default:
		fmt.Println("   触发者是 8km 以上的高云（cloud_cover_high），典型形态是卷云/卷层云。")
		fmt.Println("   薄卷云主要造成星点减光与反差下降，仍可尝试拍摄，构图上避开最厚区域。")
	}

	if len(above) > 0 {
		if (maxCCAbove < 0 || maxCCAbove < ccThr) &&
			(maxRHAbove < 0 || maxRHAbove < cfg.Thresh.RHThresholdHigh) {
			fmt.Printf("   补充：剖面在机位之上至 ~3km 这一段是干净的（最大CC=%s < %.0f%%），"+
				"但该段只属 low 层，\n", fmtMax(maxCCAbove, "%"), ccThr)
			fmt.Println("         说明不了 3km 以上的事，不构成对上面触发量的反证。")
		} else {
			fmt.Printf("   补充：剖面在机位之上至 ~3km 这一段也有云信号（最大CC=%s，最大RH=%s），"+
				"低云同样压顶。\n", fmtMax(maxCCAbove, "%"), fmtMax(maxRHAbove, "%"))
		}
	}

	lowStr := optStr(surface.CloudCoverLow, 0, "%")
	lowHit := surface.CloudCoverLow.GE(ccThr)
	belowSignal := len(below) > 0 &&
		(maxCCBelow >= ccThr || maxRHBelow >= cfg.Thresh.RHThresholdLow)
	switch {
	case lowHit && belowSignal:
		fmt.Printf("   脚下云海：cloud_cover_low=%s（≥%.0f%%）且机位之下剖面有云信号"+
			"（最大CC=%s，最大RH=%s）→ 两路证据一致，很可能有云海。\n",
			lowStr, ccThr, fmtMax(maxCCBelow, "%"), fmtMax(maxRHBelow, "%"))
	case lowHit:
		fmt.Printf("   脚下云海：cloud_cover_low=%s（≥%.0f%%），但机位之下剖面无明显云信号"+
			"（最大CC=%s）→ 低云可能在远处谷地，未必在脚下。\n",
			lowStr, ccThr, fmtMax(maxCCBelow, "%"))
	case belowSignal:
		fmt.Printf("   脚下云海：机位之下剖面有云信号（最大CC=%s，最大RH=%s），"+
			"而 cloud_cover_low=%s 未达 %.0f%%\n",
			fmtMax(maxCCBelow, "%"), fmtMax(maxRHBelow, "%"), lowStr, ccThr)
		fmt.Println("         → 格点低云覆盖率偏低，云海范围可能有限。")
	default:
		fmt.Printf("   脚下云海：cloud_cover_low=%s、机位之下剖面最大CC=%s，两路都无信号 → 脚下无云。\n",
			lowStr, fmtMax(maxCCBelow, "%"))
	}
}

func printVeilTrigger(field, thName, role string, v model.OptFloat, threshold float64) {
	verdict := "未过阈"
	switch {
	case !v.Valid:
		verdict = "缺测 → 不触发（缺测不构成肯定判据，绝不当 0%）"
	case v.GE(threshold):
		verdict = "★ 过阈 → 触发降级"
	}
	fmt.Printf("      %s = %-10s vs %s=%.0f%%  → %s   [%s]\n",
		field, optStr(v, 0, "%"), thName, threshold, verdict, role)
}

func fmtMax(v float64, unit string) string {
	if v < 0 {
		return "无有效样本"
	}
	return model.FormatFixed(v, 0) + unit
}
