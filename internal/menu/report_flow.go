package menu

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
	"github.com/prophetcro/astro-mountain/internal/report"
)

type reportForm struct {
	usePeak bool
	peak    string
	days    int
	start   string
	end     string

	// mode 运行模式：空或 "meteor" 为流星雨（默认）；"sunrise" 为日出云海模式。
	mode        string
	sunriseDate string

	allSites bool
	picked   []config.Site

	wantMarkdown bool
	wantDouyin   bool
	wantCSV      bool
	wantJSON     bool

	source    core.Source
	models    string
	outDir    string
	noCache   bool
	verbose   bool
	showTable bool
}

func (s *state) newReportForm() reportForm {
	if s.lastReport != nil {
		f := *s.lastReport

		f.picked = nil
		f.allSites = true
		return f
	}
	days := s.cfg.Output.DefaultDays
	if days <= 0 {
		days = 5
	}
	today := s.now()
	return reportForm{
		usePeak:      true,
		peak:         today.Format(dateLayout),
		days:         days,
		start:        today.Format(dateLayout),
		end:          today.AddDate(0, 0, 3).Format(dateLayout),
		mode:         "meteor",
		sunriseDate:  today.Format(dateLayout),
		allSites:     true,
		wantMarkdown: true,
		wantDouyin:   s.cfg.Output.AutoDouyin,
		wantCSV:      s.cfg.Output.ExportCSV,
		wantJSON:     s.cfg.Output.ExportJSON,
		source:       core.DefaultSource,
		models:       s.cfg.API.Models,
		outDir:       s.outDir(),
		noCache:      !s.cfg.API.CacheEnabled,
	}
}

func (s *state) reportFlow() error {
	f := s.newReportForm()
	step := 1

	for {
		var err error
		switch step {
		case 1:
			err = s.askDateRange(&f)
			if errors.Is(err, errBack) {
				return errBack
			}
			if err == nil {
				step = 2
			}
		case 2:
			err = s.askSiteSelection(&f)
			if errors.Is(err, errBack) {
				step, err = 1, nil
			} else if err == nil {
				step = 3
			}
		case 3:
			err = s.askExportOptions(&f)
			if errors.Is(err, errBack) {
				step, err = 2, nil
			} else if err == nil {
				step = 4
			}
		case 4:
			err = s.askAdvanced(&f)
			if errors.Is(err, errBack) {
				step, err = 3, nil
			} else if err == nil {
				step = 5
			}
		case 5:
			var action string
			action, err = s.confirmSummary(&f)
			switch {
			case err != nil:
				if errors.Is(err, errBack) {
					return errBack
				}
			case action == "Y":
				saved := f
				s.lastReport = &saved
				return s.execute(&f)
			case action == "n":
				step = 1
			default:
				return errBack
			}
		}
		if err != nil {
			return err
		}
	}
}

func (s *state) askDateRange(f *reportForm) error {
	u := s.u
	u.banner("[1] 生成评估报告")
	u.step("步骤 1/4：日期范围")
	u.info("[1] 流星雨极大日 + 往前推 N 天      （对应 --peak / --days）")
	u.info("[2] 自定义起止日期区间              （对应 --start / --end）")
	u.info(fmt.Sprintf("[3] 用配置里的默认天数 %d 天（以今天为极大日）", s.defaultDays()))
	u.info("[4] 日出云海模式：指定日出当天日期  （对应 --mode sunrise / --sunrise-date）")
	u.info("[b] 返回主菜单")

	def := "1"
	if f.mode == "sunrise" {
		def = "4"
	} else if !f.usePeak {
		def = "2"
	}
	pick, err := u.choice("请选择", []string{"1", "2", "3", "4", "b"}, def, backReturns)
	if err != nil {
		return err
	}

	switch pick {
	case "b":
		return errBack

	case "3":
		f.mode = "meteor"
		f.usePeak = true
		f.peak = s.now().Format(dateLayout)
		f.days = s.defaultDays()

	case "1":
		f.mode = "meteor"
		f.usePeak = true
		peak, derr := u.askDate("极大日  YYYY-MM-DD", f.peak)
		if derr != nil {
			return derr
		}
		days, ierr := u.askInt("往前推天数  0-16", 0, 16, f.days, true)
		if ierr != nil {
			return ierr
		}
		f.peak, f.days = peak, days

	case "2":
		f.mode = "meteor"
		f.usePeak = false
		for {
			start, derr := u.askDate("起始日期  YYYY-MM-DD", f.start)
			if derr != nil {
				return derr
			}
			end, derr := u.askDate("结束日期  YYYY-MM-DD", f.end)
			if derr != nil {
				return derr
			}
			st, _ := ValidateDate(start)
			en, _ := ValidateDate(end)
			if en.Before(st) {
				u.fail("结束日期不能早于起始日期")
				continue
			}
			if span := int(en.Sub(st).Hours() / 24); span > 16 {
				u.fail(fmt.Sprintf("起止跨度 %d 天超过 Open-Meteo 的 16 天预报上限", span))
				continue
			}
			f.start, f.end = start, end
			break
		}

	case "4":
		f.mode = "sunrise"
		f.usePeak = false
		// 日出模式强制 Open-Meteo（A 轨，需要气压层剖面），菜单里锁定该源。
		f.source = core.SourceOpenMeteo
		sd, derr := u.askDate("日出当天  YYYY-MM-DD", f.sunriseDate)
		if derr != nil {
			return derr
		}
		f.sunriseDate = sd
	}

	nights := f.nights()
	if len(nights) == 0 {
		u.fail("未能推导出任何观测夜，请重新选择日期")
		return errBack
	}
	u.ok(fmt.Sprintf("已确定观测夜：%s ~ %s，共 %d 夜",
		nights[0], nights[len(nights)-1], len(nights)))

	if warn := s.forecastRangeWarning(f); warn != "" {
		u.warn(warn)
		yes, cerr := u.confirm("仍要继续？", false)
		if cerr != nil {
			return cerr
		}
		if !yes {
			return errBack
		}
	}
	return nil
}

func (s *state) defaultDays() int {
	if d := s.cfg.Output.DefaultDays; d > 0 {
		return d
	}
	return 5
}

func (f reportForm) nights() []string {
	if f.mode == "sunrise" {
		sd, err := ValidateDate(f.sunriseDate)
		if err != nil {
			return nil
		}
		// 观测夜 = 日出当天回拨一天（NightIDOf 口径）。
		return []string{sd.AddDate(0, 0, -1).Format(dateLayout)}
	}
	if f.usePeak {
		peak, err := ValidateDate(f.peak)
		if err != nil {
			return nil
		}
		out := make([]string, 0, f.days+1)
		for d := f.days; d >= 0; d-- {
			out = append(out, peak.AddDate(0, 0, -d).Format(dateLayout))
		}
		return out
	}
	st, err1 := ValidateDate(f.start)
	en, err2 := ValidateDate(f.end)
	if err1 != nil || err2 != nil || en.Before(st) {
		return nil
	}
	out := make([]string, 0, 8)
	for d := st; !d.After(en); d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(dateLayout))
	}
	return out
}

func (s *state) forecastRangeWarning(f *reportForm) string {
	nights := f.nights()
	if len(nights) == 0 {
		return ""
	}
	start, err1 := ValidateDate(nights[0])
	last, err2 := ValidateDate(nights[len(nights)-1])
	if err1 != nil || err2 != nil {
		return ""
	}

	if err := core.CheckForecastRange(start, last.AddDate(0, 0, 1), s.now()); err != nil {
		return "该日期超出预报时效，可能没有任何有效数据：" + err.Error()
	}
	return ""
}

func (s *state) askSiteSelection(f *reportForm) error {
	u := s.u
	enabled := s.enabledSites()
	u.step("步骤 2/4：点位选择")

	if len(enabled) == 0 {
		u.fail("当前没有任何启用的点位，请先到 [3] 点位配置管理里启用或新增")
		return errBack
	}

	u.info(fmt.Sprintf("[1] 全部 %d 个启用点位（默认）", len(enabled)))
	u.info("[2] 手动勾选")
	u.info("[b] 上一步")
	pick, err := u.choice("请选择", []string{"1", "2", "b"}, "1", backReturns)
	if err != nil {
		return err
	}
	if pick == "b" {
		return errBack
	}
	if pick == "1" {
		f.allSites = true
		f.picked = enabled
		u.ok(fmt.Sprintf("已选全部 %d 个点位", len(enabled)))
		return nil
	}

	f.allSites = false
	selected := make([]bool, len(enabled))
	for i := range selected {
		selected[i] = true
	}
	invalid := 0
	for {
		s.printSiteChoiceTable(enabled, selected)
		count := 0
		for _, on := range selected {
			if on {
				count++
			}
		}
		text, perr := u.prompt(fmt.Sprintf(
			"输入序号切换（如 1,3,5 或 1-4）；all=全选 none=全不选；回车确认（已选 %d 个）", count),
			"", backReturns)
		if perr != nil {
			return perr
		}
		if text == "" {
			if count == 0 {
				u.fail("请至少选择一个点位")
				invalid++
				if invalid >= maxInvalidTries {
					u.warn("输入多次无效，已返回上级")
					return errBack
				}
				continue
			}
			break
		}
		idx, ierr := ParseIndexSpec(text, len(enabled))
		if ierr != nil {
			u.fail(ierr.Error())
			invalid++
			if invalid >= maxInvalidTries {
				u.warn("输入多次无效，已返回上级")
				return errBack
			}
			continue
		}
		invalid = 0
		switch strings.ToLower(text) {
		case "all", "a":
			for i := range selected {
				selected[i] = true
			}
		case "none":
			for i := range selected {
				selected[i] = false
			}
		default:
			for _, i := range idx {
				selected[i-1] = !selected[i-1]
			}
		}
	}

	f.picked = nil
	names := make([]string, 0, len(enabled))
	for i, on := range selected {
		if on {
			f.picked = append(f.picked, enabled[i])
			names = append(names, enabled[i].Name)
		}
	}
	u.ok(fmt.Sprintf("已选 %d 个点位：%s", len(names), strings.Join(names, " / ")))
	return nil
}

func (s *state) printSiteChoiceTable(sites []config.Site, selected []bool) {
	rows := make([][]string, 0, len(sites))
	for i, site := range sites {
		rows = append(rows, []string{
			fmt.Sprintf("%d", i+1),
			site.Name,
			fmt.Sprintf("%.4f", site.Lat),
			fmt.Sprintf("%.4f", site.Lon),
			fmt.Sprintf("%.1f", site.Alt),
			checkbox(selected[i]),
		})
	}
	s.u.blank()
	s.u.table("   ",
		[]string{"序号", "点位", "纬度", "经度", "海拔(m)", "选中"},
		[]string{report.AlignRight, report.AlignLeft, report.AlignRight,
			report.AlignRight, report.AlignRight, report.AlignLeft},
		rows)
	s.u.blank()
}

func (s *state) askExportOptions(f *reportForm) error {
	u := s.u
	u.step("步骤 3/4：导出内容")

	labels := []string{"Markdown 报告", "抖音竖版图", "CSV 明细", "JSON 明细"}
	hints := []string{
		"（关闭对应 --no-report）",
		"（关闭对应 --no-douyin）",
		"（开启对应 --csv）",
		"（开启对应 --json）",
	}
	selected := []bool{f.wantMarkdown, f.wantDouyin, f.wantCSV, f.wantJSON}

	if err := u.multiSelect(labels, hints, selected, true, "请至少选择一种导出内容"); err != nil {
		return err
	}
	f.wantMarkdown, f.wantDouyin, f.wantCSV, f.wantJSON =
		selected[0], selected[1], selected[2], selected[3]

	if f.wantDouyin && !f.wantMarkdown {
		u.warn("抖音图依赖 Markdown 报告的正文，已自动同时开启 Markdown 报告")
		f.wantMarkdown = true
	}
	if f.wantDouyin && s.fontErr != nil {
		u.warn("未探测到可用中文字体，抖音出图很可能失败（报告不受影响）")
		u.info("  可到 [4] 运行参数设置 → 中文字体路径 指定一个 .ttf/.ttc 文件")
	}

	var picked []string
	for i, on := range selected {
		if on {
			picked = append(picked, labels[i])
		}
	}
	u.ok("导出：" + strings.Join(picked, " + "))
	return nil
}

func (s *state) askAdvanced(f *reportForm) error {
	u := s.u
	for {
		u.step("步骤 4/4：高级选项  （回车跳过，使用当前配置）")
		u.info("[1] 数据源      " + sourceLine(f.effectiveSource()))
		u.info("[2] 气象模式    " + f.models)
		u.info("[3] 输出目录    " + f.outDir)
		u.info("[4] HTTP 缓存   " + onOff(!f.noCache) +
			fmt.Sprintf("（%d 秒）", s.cfg.API.CacheExpireS))
		u.info("[5] 详细日志    " + onOff(f.verbose))
		u.info("[6] 终端报表    " + onOff(f.showTable) + "（在菜单里打印完整逐时表格）")
		u.info("[b] 上一步")

		text, err := u.prompt("输入序号修改，回车跳过", "", backReturns)
		if err != nil {
			return err
		}
		switch text {
		case "":
			return nil
		case "b", "B":
			return errBack
		case "1":
			if aerr := s.askSource(f); aerr != nil {
				if errors.Is(aerr, errBack) {
					continue
				}
				return aerr
			}
		case "2":
			v, aerr := u.askChoice("气象模式", f.models, modelOptions())
			if aerr != nil {
				if errors.Is(aerr, errBack) {
					continue
				}
				return aerr
			}
			f.models = v
		case "3":
			v, aerr := u.askText("输出目录", f.outDir, nil)
			if aerr != nil {
				if errors.Is(aerr, errBack) {
					continue
				}
				return aerr
			}
			f.outDir = v
		case "4":
			f.noCache = !f.noCache
		case "5":
			f.verbose = !f.verbose
		case "6":
			f.showTable = !f.showTable
		default:
			u.fail(fmt.Sprintf("%q 不是可选项（1-6 / b / 回车）", text))
		}
	}
}

func (f *reportForm) effectiveSource() core.Source {
	if f.mode == "sunrise" {
		// 日出模式强制 A 轨，菜单所有数据源展示统一为 Open-Meteo。
		return core.SourceOpenMeteo
	}
	if f.source == "" {
		return core.DefaultSource
	}
	return f.source
}

func sourceLine(src core.Source) string {
	line := src.Label()
	if src.IsDefault() {
		line += "（默认）"
	}
	if hint := src.Hint(); hint != "" {
		line += "  — " + hint
	}
	return line
}

func (s *state) tomorrowUnavailableReason(src core.Source) string {
	return core.TomorrowUnavailableReason(src, s.cfg, s.opts.Engine.TomorrowDeliverable())
}

func (s *state) sourceMenuLine(src core.Source) string {
	line := sourceLine(src)
	if s.sourceUnavailableReason(src) != "" {
		line += "   ⛔ 本版不可用"
	}
	return line
}

// sourceUnavailableReason 汇总各副源（B/C 轨）的不可用原因，供菜单标注。
// 任一副源返回非空即说明用户点名该源时本轮无法交付。
func (s *state) sourceUnavailableReason(src core.Source) string {
	if r := s.tomorrowUnavailableReason(src); r != "" {
		return r
	}
	return core.MeteoblueUnavailableReason(src, s.cfg, s.opts.Engine.MeteoblueDeliverable())
}

func (s *state) askSource(f *reportForm) error {
	u := s.u

	for tries := 0; tries < maxInvalidTries; tries++ {
		u.blank()
		for _, src := range core.AllSources() {
			u.info("  " + s.sourceMenuLine(src))
		}

		u.warn(core.TomorrowQuotaNotice())
		if reason := s.tomorrowUnavailableReason(core.SourceTomorrow); reason != "" {
			u.warn("Tomorrow.io 本版不可用：" + reason)
		}
		if reason := core.MeteoblueUnavailableReason(
			core.SourceMeteoblue, s.cfg, s.opts.Engine.MeteoblueDeliverable()); reason != "" {
			u.warn("Meteoblue 本版不可用：" + reason)
		}

		pick, err := u.choice("数据源", []string{
			string(core.SourceOpenMeteo), string(core.SourceTomorrow), string(core.SourceMeteoblue),
		}, string(f.effectiveSource()), backReturns)
		if err != nil {
			return err
		}

		src, perr := core.ParseSource(pick)
		if perr != nil {

			u.fail(perr.Error())
			return nil
		}

		if s.tomorrowUnavailableReason(src) != "" {
			u.warn("未采用 " + src.Label() + "：它在本版不可用（原因见上方提示）。")
			u.info("请改选其他数据源，保持 " + f.effectiveSource().Label() +
				" 直接回车即可。")
			continue
		}

		f.source = src
		u.ok("数据源：" + sourceLine(src))
		return nil
	}

	u.warn("多次选择了不可用的数据源，已返回上级；当前仍为 " +
		f.effectiveSource().Label())
	return errBack
}

func onOff(v bool) string {
	if v {
		return "开启"
	}
	return "关闭"
}

func (s *state) confirmSummary(f *reportForm) (string, error) {
	u := s.u
	nights := f.nights()
	sites := f.sites(s)

	names := make([]string, 0, len(sites))
	for _, site := range sites {
		names = append(names, site.Name)
	}
	nameLine := strings.Join(names, " / ")
	if len(names) > 6 {
		nameLine = strings.Join(names[:6], " / ") +
			fmt.Sprintf(" …（共 %d 个，余 %d 个未列出）", len(names), len(names)-6)
	}

	var exports []string
	if f.wantMarkdown {
		exports = append(exports, "Markdown 报告")
	}
	if f.wantDouyin {
		exports = append(exports, "抖音竖版图")
	}
	if f.wantCSV {
		exports = append(exports, "CSV")
	}
	if f.wantJSON {
		exports = append(exports, "JSON")
	}

	u.blank()
	u.println(" ── 确认 " + report.Repeat("─", boxWidth-6))
	modeLabel := "流星雨"
	if f.mode == "sunrise" {
		modeLabel = "日出云海（--mode sunrise）"
	}
	u.printf("   模式     %s\n", modeLabel)
	if len(nights) > 0 {
		u.printf("   观测夜   %s ~ %s   （%d 夜）\n",
			nights[0], nights[len(nights)-1], len(nights))
	}
	u.printf("   点位     %d 个：%s\n", len(sites), nameLine)
	u.printf("   数据源   %s\n", sourceLine(f.effectiveSource()))
	u.printf("   气象模式 %s\n", f.models)
	u.printf("   导出     %s\n", strings.Join(exports, " + "))
	u.printf("   输出至   %s\n", f.outDir)
	u.printf("   等价命令 %s\n", s.equivalentCommand(f))
	u.println(" " + report.Repeat("─", boxWidth+1))
	u.info("Y=执行   n=重填参数   b=返回主菜单")

	pick, err := u.choice("确认执行？", []string{"Y", "n", "b"}, "Y", backReturns)
	if err != nil {
		return "", err
	}
	return pick, nil
}

func (s *state) equivalentCommand(f *reportForm) string {
	parts := []string{"astro-mountain"}
	switch f.mode {
	case "sunrise":
		parts = append(parts, "--mode sunrise", "--sunrise-date "+f.sunriseDate)
	default:
		if f.usePeak {
			parts = append(parts, "--peak "+f.peak, fmt.Sprintf("--days %d", f.days))
		} else {
			parts = append(parts, "--start "+f.start, "--end "+f.end)
		}
	}

	if src := f.effectiveSource(); !src.IsDefault() {
		parts = append(parts, "--source "+string(src))
	}
	if f.models != "" && f.models != s.cfg.API.Models {
		parts = append(parts, "--models "+f.models)
	}
	if f.allSites && s.sitesPath != "" {
		parts = append(parts, "--sites "+s.sitesPath)
	}
	if f.wantCSV {
		parts = append(parts, "--csv")
	}
	if f.wantJSON {
		parts = append(parts, "--json")
	}
	if !f.wantMarkdown {
		parts = append(parts, "--no-report")
	}
	if !f.wantDouyin {
		parts = append(parts, "--no-douyin")
	}
	if f.noCache {
		parts = append(parts, "--no-cache")
	}
	if f.verbose {
		parts = append(parts, "--verbose")
	}
	if f.outDir != "" {
		parts = append(parts, "--out-dir "+f.outDir)
	}
	cmd := strings.Join(parts, " ")
	if !f.allSites {
		cmd += "\n            # 注：本次为菜单手动勾选点位，CLI 需自备只含这些点位的 sites.json"
	}
	return cmd
}

func (f reportForm) sites(s *state) []config.Site {
	if f.allSites || len(f.picked) == 0 {
		return s.enabledSites()
	}
	return f.picked
}

func (s *state) execute(f *reportForm) error {
	u := s.u

	if f.mode != "sunrise" {
		if reason := s.tomorrowUnavailableReason(f.effectiveSource()); reason != "" {
			u.blank()
			u.fail("无法执行：" + reason)
			u.info("未生成任何报告——不会用 " + core.DefaultSource.Label() +
				"（A 轨）替你出一份你没要的报告。")
			return u.pause()
		}
	}

	if s.opts.Engine == nil {
		u.blank()
		u.fail("执行内核未注入（Options.Engine 为 nil），无法执行评估")
		return u.pause()
	}

	sites := f.sites(s)
	params := core.RunParams{
		Mode:       f.mode,
		Source:     f.effectiveSource(),
		Models:     f.models,
		Sites:      sites,
		SitesPath:  s.sitesPath,
		NoCache:    f.noCache,
		OutDir:     f.outDir,
		ExportCSV:  f.wantCSV,
		ExportJSON: f.wantJSON,
		NoReport:   !f.wantMarkdown,
		Douyin:     f.wantDouyin,
		Stdout:     u.out,
		Quiet:      !f.showTable,
		Verbose:    f.verbose,
	}
	switch f.mode {
	case "sunrise":
		// 日出模式必须以 Open-Meteo（A 轨）取气压层剖面反演云海几何，
		// 无视菜单里可能选过的 tomorrow/meteoblue。
		params.SunriseDate = f.sunriseDate
		params.Source = core.SourceOpenMeteo
		params.Peak, params.Start, params.End = "", "", ""
	default:
		if f.usePeak {
			params.Peak, params.Days = f.peak, f.days
		} else {
			params.Start, params.End = f.start, f.end
		}
	}

	u.blank()
	u.info(fmt.Sprintf("正在获取 %d 个点位、%d 个观测夜的气象数据…（首次运行需要几十秒）",
		len(sites), len(f.nights())))

	engine := s.opts.Engine
	prevLogf := engine.Logf
	engine.Logf = func(format string, args ...any) {
		u.printf("   · "+format+"\n", args...)
	}

	started := time.Now()
	res := engine.Run(s.ctx, params)
	engine.Logf = prevLogf

	u.blank()
	u.printf("  执行完毕，耗时 %.1f 秒\n", time.Since(started).Seconds())
	s.printExecResult(res)

	return u.pause()
}

func (s *state) printExecResult(res core.ExecResult) {
	u := s.u

	u.blank()
	u.println(" ── 产出 " + report.Repeat("─", boxWidth-6))
	produced := false
	if res.ReportPath != "" {
		u.printf("   报告   %s\n", res.ReportPath)
		produced = true
	}
	if res.CSVPath != "" {
		u.printf("   CSV    %s\n", res.CSVPath)
		produced = true
	}
	if res.JSONPath != "" {
		u.printf("   JSON   %s\n", res.JSONPath)
		produced = true
	}
	if n := len(res.ImagePaths); n > 0 {
		u.printf("   抖音图 共 %d 张\n", n)
		for i, p := range res.ImagePaths {
			if i >= 5 {
				u.printf("            …（其余 %d 张省略）\n", n-5)
				break
			}
			u.printf("            %s\n", p)
		}
		produced = true
	}
	if !produced {
		u.info("  （本次没有生成任何文件）")
	}

	if n := len(res.Rows); n > 0 {
		valid := 0
		for i := range res.Rows {
			if res.Rows[i].HasData {
				valid++
			}
		}
		u.printf("   数据   %d 个夜间时次，其中 %d 个有有效预报\n", n, valid)
	}

	if len(res.Warnings) > 0 {
		u.blank()
		u.printf("  ⚠ %d 条警告（不影响已生成的产物）：\n", len(res.Warnings))
		for _, w := range res.Warnings {
			u.printf("     · %s\n", w)
		}
	}
	if len(res.Errors) > 0 {
		u.blank()
		u.printf("  ✗ %d 条错误：\n", len(res.Errors))
		for _, e := range res.Errors {
			u.printf("     · %s\n", e)
		}
	}
	if res.ExitCode == 0 && len(res.Errors) == 0 {
		u.blank()
		u.ok("本次评估执行成功")
	}
}
