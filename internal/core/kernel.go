// Package core 是星空气象预报的分析内核。
//
// 它把「点位 + 时间窗 + 数据源」变成一次完整运行：解析观测夜区间、抓取预报、
// 按夜间窗口逐小时评级、可选装配 B 轨（Tomorrow.io）双轨结果、打印终端报告，
// 并导出 Markdown / CSV / JSON 与抖音竖图。
//
// 命令行解析与交互菜单都在上层（cli、menu），它们只通过 Engine.Run 进入本包；
// 反过来 core 不感知任何终端 UI。
package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/report"
)

// DateLayout 是命令行日期参数与夜 ID 统一使用的布局。
const DateLayout = "2006-01-02"

// Engine 承载一次（或多次）运行所需的配置与协作者。
//
// 除 Cfg 外的字段都允许为零值，Run 会各自兜底：Client 为 nil 时按配置自建，
// Renderer 为 nil 表示不出图，TomorrowFetcher 为 nil 表示 B 轨未接线，
// MeteoblueFetcher 为 nil 表示 C 轨未接线，Logf 为 nil 表示不记进度日志。
type Engine struct {
	Cfg    config.Config
	Client *api.Client

	Renderer ImageRenderer

	TomorrowFetcher TomorrowFetcher

	// MeteoblueFetcher 是 C 轨取数器；为 nil 表示 C 轨未接线。
	// C 轨直接产出与 A 轨维度兼容的 []HourRow，并入主 rows，不另起渲染分支。
	MeteoblueFetcher MeteoblueFetcher

	Stdout io.Writer

	// Now 可注入以冻结「今天」，便于测试预报窗口校验。
	Now func() time.Time

	// Logf 是可选的进度日志钩子，输出面向使用者而非调试。
	Logf func(format string, args ...any)
}

// NewEngine 用给定配置构造 Engine，默认写 os.Stdout、用真实时钟。
// Client、Renderer、TomorrowFetcher 由调用方按需注入。
func NewEngine(cfg config.Config) *Engine {
	return &Engine{
		Cfg:    cfg,
		Stdout: os.Stdout,
		Now:    time.Now,
	}
}

// logf 转发到可选日志钩子，未注入时静默丢弃。
func (e *Engine) logf(format string, args ...any) {
	if e.Logf != nil {
		e.Logf(format, args...)
	}
}

// now 返回当前时间，未注入时钟时用 time.Now。
func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// ResolveRange 把日期参数解析成取数区间与观测夜列表。
//
// 支持两种模式：Peak 模式以极大日为最后一夜、向前追溯 p.Days 天；
// Start/End 模式按给定区间逐夜展开（End 当天不单独算一夜）。
// 两种模式都没给足参数时返回错误。
//
// 返回的 start/end 按 UTC 解析（Peak 模式下 end 自动取最后一夜的次日，
// 以覆盖跨零点的后半夜）；nights 是 YYYY-MM-DD 夜 ID 列表；
// desc 是给终端与报告用的区间描述文案。
func ResolveRange(p RunParams, w config.WindowConfig) (start, end time.Time,
	nights []string, desc string, err error) {

	nightWindow := fmt.Sprintf("每夜 %02d:00 → 次日 %02d:00", w.NightStartHour, w.NightEndHour)

	// 日出云海模式：以「日出当天」为锚，分析其前一夜（含日出时分）。
	// 抓数区间覆盖前一夜 00:00 到日出当天 +1 日 00:00，确保傍晚与清晨都被纳入；
	// 观测夜 ID 取日出当天回拨一天（NightIDOf 口径）。
	if p.Mode == "sunrise" {
		if p.SunriseDate == "" {
			return start, end, nil, "", fmt.Errorf("日出模式（--mode sunrise）必须指定 --sunrise-date（日出当天 YYYY-MM-DD）")
		}
		sd, perr := time.ParseInLocation(DateLayout, p.SunriseDate, time.UTC)
		if perr != nil {
			return start, end, nil, "", fmt.Errorf("--sunrise-date 日期格式应为 YYYY-MM-DD：%w", perr)
		}
		targetNight := sd.AddDate(0, 0, -1)
		start = targetNight
		end = sd.AddDate(0, 0, 1)
		nights = []string{targetNight.Format(DateLayout)}
		desc = fmt.Sprintf("%s 前一夜（含 %s 日出时分）共 1 夜（%s；日出当天 %s）",
			targetNight.Format(DateLayout), sd.Format(DateLayout), nightWindow, p.SunriseDate)
		return start, end, nights, desc, nil
	}

	if p.Peak != "" {
		peak, perr := time.ParseInLocation(DateLayout, p.Peak, time.UTC)
		if perr != nil {
			return start, end, nil, "", fmt.Errorf("--peak 日期格式应为 YYYY-MM-DD：%w", perr)
		}
		if p.Days < 0 {
			return start, end, nil, "", fmt.Errorf("--days 不能为负数")
		}
		nightDates := make([]time.Time, 0, p.Days+1)
		for d := p.Days; d >= 0; d-- {
			nightDates = append(nightDates, peak.AddDate(0, 0, -d))
		}
		start = nightDates[0]

		end = nightDates[len(nightDates)-1].AddDate(0, 0, 1)
		nights = formatDates(nightDates)
		desc = fmt.Sprintf("%s ~ %s 共 %d 夜（%s；极大日 %s，含极大前 %d 天）",
			nights[0], nights[len(nights)-1], len(nights), nightWindow,
			peak.Format(DateLayout), p.Days)
		return start, end, nights, desc, nil
	}

	if p.Start == "" || p.End == "" {
		return start, end, nil, "", fmt.Errorf("必须指定 --peak，或同时指定 --start 与 --end")
	}
	start, err = time.ParseInLocation(DateLayout, p.Start, time.UTC)
	if err != nil {
		return start, end, nil, "", fmt.Errorf("--start 日期格式应为 YYYY-MM-DD：%w", err)
	}
	end, err = time.ParseInLocation(DateLayout, p.End, time.UTC)
	if err != nil {
		return start, end, nil, "", fmt.Errorf("--end 日期格式应为 YYYY-MM-DD：%w", err)
	}
	if end.Before(start) {
		return start, end, nil, "", fmt.Errorf("--end 不能早于 --start")
	}
	span := int(end.Sub(start).Hours() / 24)
	nightDates := make([]time.Time, 0, span+1)
	for d := 0; d < span; d++ {
		nightDates = append(nightDates, start.AddDate(0, 0, d))
	}
	// start == end 时上面一夜都排不出来，兜底至少给一夜，避免空区间。
	if len(nightDates) == 0 {
		nightDates = append(nightDates, start)
	}
	nights = formatDates(nightDates)
	desc = fmt.Sprintf("%s ~ %s 共 %d 夜（%s）",
		nights[0], nights[len(nights)-1], len(nights), nightWindow)
	return start, end, nights, desc, nil
}

// CheckForecastRange 校验请求区间是否落在数据源的可用预报窗口内：
// 最远不超过 api.ForecastMaxAheadDays 天，最早不早于 api.ForecastMaxPastDays 天。
// today 只取其日期部分（UTC）参与比较；超窗时返回带处置建议的错误。
func CheckForecastRange(start, end, today time.Time) error {
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	maxAhead := today.AddDate(0, 0, api.ForecastMaxAheadDays)
	minPast := today.AddDate(0, 0, -api.ForecastMaxPastDays)
	if end.After(maxAhead) {
		return fmt.Errorf("结束日期 %s 超出 Open-Meteo 预报范围（最远 %s）。"+
			"流星雨极大若还很远，请临近 %d 天内再跑。",
			end.Format(DateLayout), maxAhead.Format(DateLayout), api.ForecastMaxAheadDays)
	}
	if start.Before(minPast) {
		return fmt.Errorf("起始日期 %s 超出可回溯范围（最早 %s）。",
			start.Format(DateLayout), minPast.Format(DateLayout))
	}
	return nil
}

func formatDates(dates []time.Time) []string {
	out := make([]string, 0, len(dates))
	for _, d := range dates {
		out = append(out, d.Format(DateLayout))
	}
	return out
}

// Run 执行一次完整分析，任何失败都收敛进返回值而不是 panic 或直接退出进程：
// 错误进 res.Errors、可容忍的问题进 res.Warnings，ExitCode 给调用方作退出码。
//
// 主流程：校验参数与预报窗口 → 解析点位 → 逐点位取数、评级（可选叠加 B 轨）
// → 结果自检 → 打印终端报告 → 导出文件 → 可选出图。
//
// 两条不让步的规矩：
//   - 用户点名 B 轨却不可用时立即中止（退出码 2），不拿 A 轨结果顶替；
//   - 单个点位取数失败只降级为警告，不连累其它点位。
func (e *Engine) Run(ctx context.Context, p RunParams) ExecResult {
	var res ExecResult

	start, end, nights, nightsDesc, err := ResolveRange(p, e.Cfg.Window)
	if err != nil {
		res.Errors = append(res.Errors, "参数错误："+err.Error())
		res.ExitCode = 2
		return res
	}

	// 日出云海模式走独立链路，不进入 HourRow / 双模型对比 / B·C 轨管线。
	if p.Mode == "sunrise" {
		return e.runSunrise(ctx, p, start, end, nights, nightsDesc)
	}

	if err := CheckForecastRange(start, end, e.now()); err != nil {
		res.Errors = append(res.Errors, "参数错误："+err.Error())
		res.ExitCode = 2
		return res
	}

	if reason := TomorrowUnavailableReason(p.Source, e.Cfg, e.TomorrowDeliverable()); reason != "" {
		res.Errors = append(res.Errors, "参数错误：--source tomorrow 无法生效——"+reason)
		res.Errors = append(res.Errors,
			"已中止，未生成任何报告：不会用 Open-Meteo（A 轨）替你出一份你没要的报告。")
		res.ExitCode = 2
		return res
	}

	if reason := MeteoblueUnavailableReason(p.Source, e.Cfg, e.MeteoblueDeliverable()); reason != "" {
		res.Errors = append(res.Errors, "参数错误：--source meteoblue 无法生效——"+reason)
		res.Errors = append(res.Errors,
			"已中止，未生成任何报告：不会用 Open-Meteo（A 轨）替你出一份你没要的报告。")
		res.ExitCode = 2
		return res
	}

	useTomorrow := UseTomorrow(p.Source, e.Cfg)
	useMeteoblue := UseMeteoblue(p.Source, e.Cfg)

	sites, warns, err := e.resolveSites(p)
	res.Warnings = append(res.Warnings, warns...)
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
		res.ExitCode = 2
		return res
	}
	if len(sites) == 0 {
		res.Errors = append(res.Errors, "没有任何启用的观测点位")
		res.ExitCode = 2
		return res
	}
	res.Sites = sites

	models := p.Models
	if models == "" {
		models = e.Cfg.API.Models
	}
	if useMeteoblue {
		// Meteoblue 没有「数值模式」概念，用其融合预报身份标注，
		// 避免被误读为某个 Open-Meteo 模式。
		models = "Meteoblue 融合预报"
	}

	// 用户显式指定了模式就全站统一照用；否则允许按站点所在区域各自挑模式。
	explicitModels := p.Models != ""

	// 双模型交叉对比：config.api.cross_model 非空即默认开启，--compare 强制开，
	// --no-cross-model 强制关；B 轨（Tomorrow.io）与 C 轨（Meteoblue）都不参与对比。
	compareOn := (p.Compare || e.Cfg.API.CrossModel != "") && !p.NoCompare && !useTomorrow && !useMeteoblue

	client := e.Client
	if client == nil {
		client = api.New(e.Cfg.API, !p.NoCache, api.WithLogger(e.logf))
	}
	targetNights := make(map[string]bool, len(nights))
	for _, n := range nights {
		targetNights[n] = true
	}

	rows := make([]HourRow, 0, len(sites)*10)
	// 兜底东八区：若所有点位都取数失败，报告仍需要一个时区偏移可写。
	utcOffsetHours := 8.0

	// 报告级天文量只能挂在一个偏移上，取第一个成功点位的偏移为准。
	repOffsetHours := 8.0
	offsetSet := false
	for _, site := range sites {
		if ctx.Err() != nil {
			res.Errors = append(res.Errors, "执行被取消："+ctx.Err().Error())
			res.ExitCode = 1
			return res
		}
		siteModels := models
		if !explicitModels {
			siteModels = api.ResolveModel(site.Region, models)
		}
		var siteRows []HourRow
		if useMeteoblue {
			// C 轨：Meteoblue 直接产出与 A 轨维度兼容的逐小时评估行，并入主 rows。
			// 不回落 Open-Meteo——用户点名要的是 Meteoblue，缺数就降级而非冒充。
			mrows, merr := e.MeteoblueFetcher.FetchSite(ctx, site, start, end, targetNights)
			if merr != nil {
				// 单点失败不影响其它点位：记警告后跳过。
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("[%s] Meteoblue 获取/解析失败：%v", site.Name, merr))
				continue
			}
			siteRows = mrows
			// Meteoblue 不回传 UTCOffsetSeconds，从首个有效行的本地时间反推偏移。
			if len(siteRows) > 0 {
				utcOffsetHours = siteRows[0].Time.Sub(siteRows[0].Time.UTC()).Seconds() / 3600.0
			}
		} else {
			resp, _, err := client.FetchSite(ctx, site, start, end, siteModels)
			if err != nil {
				// 单点失败不影响其它点位：记警告后跳过。
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("[%s] 获取/解析失败：%v", site.Name, err))
				continue
			}
			utcOffsetHours = float64(resp.UTCOffsetSeconds) / 3600.0
			siteRows = AnalyseSite(site, resp, targetNights, e.Cfg)
		}

		if !offsetSet {
			repOffsetHours = utcOffsetHours
			offsetSet = true
		} else if repOffsetHours != utcOffsetHours {
			e.logf("⚠️ 站点 %s 的 UTC 偏移 %g 与首位站点 %g 不一致；"+
				"报告级天文量按首位站点偏移（UTC+%g）计算，跨时区点位请谨慎解读。",
				site.Name, utcOffsetHours, repOffsetHours, repOffsetHours)
		}
		if len(siteRows) == 0 {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("[%s] 时段内没有落在夜间窗口的数据", site.Name))
		}
		rows = append(rows, siteRows...)
		e.logf("[%s] 取得 %d 条夜间记录", site.Name, len(siteRows))

		if compareOn {
			iconRows, gfsRows, cwarns := e.resolveCompareRows(ctx, client, site, start, end, targetNights, siteModels, siteRows)
			res.Warnings = append(res.Warnings, cwarns...)
			res.Compare = append(res.Compare, PairCompareRows(site.Name, iconRows, gfsRows)...)
			e.logf("[%s] 双模型对比配对 %d 行", site.Name, len(iconRows)+len(gfsRows))
		}

		if useTomorrow {
			tr, warn := e.runTomorrowTrack(ctx, site, siteRows, utcOffsetHours)
			if warn != "" {
				res.Warnings = append(res.Warnings, warn)
			}
			if tr != nil {
				res.Tomorrow = append(res.Tomorrow, tr)
				e.logf("[%s] B 轨装配 %d 行（无数据 %d 行，配额耗尽=%v）",
					site.Name, len(tr.Rows), tr.NoDataCount(), tr.QuotaExhausted)
			}
		}
	}

	sortRows(rows)
	res.Rows = rows

	if issues := AuditRows(rows); len(issues) > 0 {
		res.Warnings = append(res.Warnings, issues...)
	}

	validCount := 0
	visibilityAvailable := false
	for i := range rows {
		if rows[i].HasData {
			validCount++
			if rows[i].Visibility.Valid {
				visibilityAvailable = true
			}
		}
	}
	// Peak / Days 只在极大日模式下写进元信息，普通区间模式留空。
	var daysPtr *int
	var peak model.NullString
	if p.Peak != "" {
		d := p.Days
		daysPtr = &d
		peak = model.Str(p.Peak)
	}
	meta := ReportMeta{
		Models:              models,
		Start:               start.Format(DateLayout),
		End:                 end.Format(DateLayout),
		Nights:              nights,
		NightsDesc:          nightsDesc,
		Peak:                peak,
		Days:                daysPtr,
		Timezone:            e.Cfg.API.Timezone,
		UTCOffsetHours:      repOffsetHours,
		Sites:               sites,
		HoursTotal:          len(rows),
		HoursWithData:       validCount,
		VisibilityAvailable: visibilityAvailable,
		GeneratedAt:         e.now().Format("2006-01-02 15:04:05"),

		Source: metaSourceOf(p.Source),

		Disclaimer: sourceDisclaimer(useTomorrow, useMeteoblue),
	}
	if meta.Timezone == "" {
		meta.Timezone = "Asia/Shanghai"
	}
	res.Meta = meta
	// meta.Nights 是「请求的夜」，res.Nights 是「真的有行的夜」，两者可能不同。
	res.Nights = nightKeys(rows)

	outDir := p.OutDir
	if outDir == "" {
		outDir = e.Cfg.Output.OutDir
	}
	if outDir == "" {
		outDir = "reports"
	}

	// 一行都没有：仍旧出一份报告留痕（记录本次请求与失败原因），但算失败。
	if len(rows) == 0 {
		res.Errors = append(res.Errors,
			"所有点位均无有效数据。请检查：网络是否可达 api.open-meteo.com、"+
				"日期是否在预报范围内、--models 是否拼写正确。")
		e.emitReport(&res, p, outDir)
		res.ExitCode = 1
		return res
	}

	if !p.Quiet {
		w := p.Stdout
		if w == nil {
			w = e.Stdout
		}
		if w == nil {
			w = os.Stdout
		}

		// A 轨与 B 轨的终端呈现是两套排版，按元信息里的数据源分流。
		if report.IsTomorrowSource(meta.Source) {
			report.PrintTomorrowHeader(w, meta, e.Cfg)
			for _, night := range report.TomorrowNightKeys(res.Tomorrow) {
				report.PrintTomorrowNightBlock(w, night, res.Tomorrow, e.Cfg)
			}
			report.PrintTomorrowOverview(w, res.Tomorrow,
				report.TomorrowNightKeys(res.Tomorrow), e.Cfg)
		} else {
			report.PrintHeader(w, meta, e.Cfg)
			for _, night := range res.Nights {
				report.PrintNightBlock(w, night, rowsOfNight(rows, night), res.Compare, sites, e.Cfg, int(repOffsetHours*3600))
			}
			report.PrintOverview(w, rows, res.Compare, sites, res.Nights, meta.Models, e.Cfg)
			if len(res.Compare) > 0 {
				report.PrintCrossModelSummary(w, res.Compare, e.Cfg)
			}
		}
	}

	if err := e.exportFiles(&res, p, outDir); err != nil {
		res.Errors = append(res.Errors, err.Error())
		res.ExitCode = 1
		return res
	}

	e.emitReport(&res, p, outDir)

	if p.Douyin {
		e.renderImages(&res, p, outDir)
	}

	// 有行但全是缺测行：报告照出，但退出码仍判失败，避免被误当成有效结论。
	if validCount == 0 {
		res.Errors = append(res.Errors,
			"所选观测夜没有任何有效预报数据。"+
				"可能原因：①数值模式未返回气压层云量等必要字段（部分模型免费层不含气压层数据，如 ecmwf_ifs04）；"+
				"②确实超出该模式预报时效。请改用 icon_seamless 或 gfs_seamless，或在极大日前 5~7 天再跑。")
		res.ExitCode = 1
		return res
	}
	res.ExitCode = 0
	return res
}

// RunDouyin 以「静默 + 出图」组合跑一次 Run：不打印终端报告，只产出文件与竖图。
func (e *Engine) RunDouyin(ctx context.Context, p RunParams) ExecResult {
	p.Quiet = true
	p.Douyin = true
	return e.Run(ctx, p)
}

// resolveSites 决定本次分析用哪些点位：注入了 Sites 就直接用（仍只取启用的），
// 否则从配置文件加载。第二个返回值是点位配置的解析警告，不致命。
func (e *Engine) resolveSites(p RunParams) ([]Site, []string, error) {
	if len(p.Sites) > 0 {
		return enabledOnly(p.Sites), nil, nil
	}
	result, err := config.LoadSites(p.SitesPath)
	if err != nil {
		return nil, nil, fmt.Errorf("加载点位失败：%w", err)
	}
	return result.Enabled(), result.Warnings, nil
}

func enabledOnly(sites []Site) []Site {
	out := make([]Site, 0, len(sites))
	for _, s := range sites {
		if s.IsEnabled() {
			out = append(out, s)
		}
	}
	return out
}

// exportFiles 按命令行与配置的并集导出 CSV / JSON（任一处要求即导出），
// 并把生成路径写回 res。导出失败视为致命错误，由调用方转成非零退出码。
func (e *Engine) exportFiles(res *ExecResult, p RunParams, outDir string) error {
	wantCSV := p.ExportCSV || e.Cfg.Output.ExportCSV
	wantJSON := p.ExportJSON || e.Cfg.Output.ExportJSON
	if !wantCSV && !wantJSON {
		return nil
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("导出失败：创建目录 %s 失败：%w", outDir, err)
	}

	// A/B 两轨的数据结构不同，导出走各自的编码器。
	tomorrow := report.IsTomorrowSource(res.Meta.Source)

	if wantCSV {
		path := filepath.Join(outDir, report.ExportFilename(res.Meta, ".csv"))
		if tomorrow {
			if err := report.ExportTomorrowCSV(path, res.Tomorrow); err != nil {
				return fmt.Errorf("导出失败：%w", err)
			}
			e.logf("已导出 B 轨 CSV：%s（%d 点位）", path, len(res.Tomorrow))
		} else {
			if err := report.ExportCSV(path, res.Rows); err != nil {
				return fmt.Errorf("导出失败：%w", err)
			}
			e.logf("已导出 CSV：%s（%d 行）", path, len(res.Rows))
		}
		res.CSVPath = path
	}
	if wantJSON {
		path := filepath.Join(outDir, report.ExportFilename(res.Meta, ".json"))
		if tomorrow {
			if err := report.ExportTomorrowJSON(path, res.Meta, res.Tomorrow, e.Cfg); err != nil {
				return fmt.Errorf("导出失败：%w", err)
			}
			e.logf("已导出 B 轨 JSON：%s（%d 点位）", path, len(res.Tomorrow))
		} else {
			if err := report.ExportJSON(path, res.Meta, res.Rows, e.Cfg); err != nil {
				return fmt.Errorf("导出失败：%w", err)
			}
			e.logf("已导出 JSON：%s（%d 行）", path, len(res.Rows))
		}
		res.JSONPath = path
	}
	return nil
}

// emitReport 生成 Markdown 报告并把路径写回 res。
// 与导出不同，报告生成失败只记警告：终端结论已经给出，不该因为写文件失败而全盘作废。
func (e *Engine) emitReport(res *ExecResult, p RunParams, outDir string) {
	if p.NoReport {
		e.logf("已指定 --no-report，跳过 Markdown 报告生成")
		return
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("Markdown 报告生成失败：创建目录 %s 失败：%v", outDir, err))
		return
	}

	var path string
	var err error
	if report.IsTomorrowSource(res.Meta.Source) {
		path, err = report.WriteTomorrowMarkdownReport(res.Tomorrow, res.Meta, e.Cfg, outDir)
	} else {
		path, err = report.WriteMarkdownReport(res.Rows, res.Compare, res.Meta, e.Cfg, outDir)
	}
	if err != nil {
		res.Warnings = append(res.Warnings, "Markdown 报告生成失败："+err.Error())
		return
	}
	res.ReportPath = path
	e.logf("已生成 Markdown 报告：%s", path)
}

// metaSourceOf 把本次选择的数据源翻译成写进 ReportMeta 的数据源标识。
//
// 三态：Tomorrow → MetaSourceTomorrow；Meteoblue → MetaSourceMeteoblue；
// 其余（含默认 Open-Meteo）→ MetaSourceOpenMeteo。
//
// 注意：Meteoblue 返回的是与 A 轨维度兼容的 HourRow，报告层按「非 Tomorrow 源」
// 走 A 轨渲染路径（IsTomorrowSource 仅对前缀 tomorrow 命中），因此 meteoblue
// 不会误入 B 轨单独的渲染分支。
func metaSourceOf(src Source) string {
	switch src {
	case SourceTomorrow:
		return report.MetaSourceTomorrow
	case SourceMeteoblue:
		return report.MetaSourceMeteoblue
	default:
		return report.MetaSourceOpenMeteo
	}
}

// sourceDisclaimer 按数据源生成免责声明，避免把 C 轨（Meteoblue）的「分层云量、
// 不反演云海几何」误写成 A 轨的「气压层反演」。
func sourceDisclaimer(useTomorrow, useMeteoblue bool) string {
	if useMeteoblue {
		// C 轨只有分层云量 + 降水 + 能见度，没有气压层，故不谈「云底/云顶反演」，
		// 以免用户误以为软件能判出云海位置。
		return "Meteoblue 分层云量不反演云海几何（无气压层），仅判通透/降水/能见度；" +
			"天文量为纯 Go 近似算法结果，均非观测实测值。"
	}
	if useTomorrow {
		return "云底高度为 Tomorrow.io 直接产品（相对模式地形，已订正到机位）；" +
			"天文量为纯 Go 近似算法结果。"
	}
	return "云底/云顶为气压层剖面反演值；天文量为纯 Go 近似算法结果。"
}

// renderImages 调用注入的渲染器出抖音竖图，失败只记警告不影响主流程。
//
// 出图以 Markdown 报告为输入，所以 --no-report 时会先生成一份临时报告，
// 用完即删。
func (e *Engine) renderImages(res *ExecResult, p RunParams, outDir string) {
	if e.Renderer == nil {
		res.Warnings = append(res.Warnings,
			"已请求抖音出图，但未注入 ImageRenderer（渲染引擎尚未接入），已跳过")
		return
	}

	// 用户显式指定了 --out-dir 时，图片跟着报告走（放其下的 douyin/ 子目录），
	// 而不是散落到配置里的全局出图目录。
	douyinDir := e.Cfg.Output.DouyinDir
	if p.OutDir != "" || douyinDir == "" {
		douyinDir = filepath.Join(outDir, "douyin")
	}

	mdPath := res.ReportPath
	if mdPath == "" {
		tmpDir, err := os.MkdirTemp("", "astro-douyin-")
		if err != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("抖音出图跳过：创建临时目录失败：%v", err))
			return
		}
		defer func() {
			// 清理失败只记日志：图已经出了，不该为此报错。
			if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
				e.logf("临时报告目录清理失败：%s：%v", tmpDir, rmErr)
			}
		}()
		tmpMD, err := report.WriteMarkdownReport(res.Rows, res.Compare, res.Meta, e.Cfg, tmpDir)
		if err != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("抖音出图跳过：--no-report 下生成临时报告失败：%v", err))
			return
		}
		mdPath = tmpMD
		e.logf("已指定 --no-report，为出图生成临时报告：%s", tmpMD)
	}

	paths, err := e.Renderer.Render(mdPath, e.Cfg, douyinDir)
	if err != nil {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("抖音出图失败（渲染器 %s）：%v", e.Renderer.Name(), err))
		return
	}
	res.ImagePaths = paths
	e.logf("已生成抖音竖图 %d 张（输出目录 %s）", len(paths), douyinDir)
}

// runSunrise 执行「日出云海模式」：取数 → 逐站点 BuildSunriseReport → 终端打印 → 写 Markdown。
// 与流星雨模式互斥，单独走这一条链路，不进入 HourRow / 双模型对比 / B·C 轨管线。
//
// 日出模式必须用气压层剖面反演云海几何，因此强制 Open-Meteo（A 轨）；
// 若调用方硬塞 Tomorrow.io / Meteoblue，直接中止（退出码 2），不拿空结果顶替。
func (e *Engine) runSunrise(ctx context.Context, p RunParams,
	start, end time.Time, nights []string, nightsDesc string) ExecResult {

	var res ExecResult
	if len(nights) == 0 {
		res.Errors = append(res.Errors, "日出模式未解析出观测夜")
		res.ExitCode = 2
		return res
	}
	targetNight := nights[0]
	sunriseDate, perr := time.ParseInLocation(DateLayout, p.SunriseDate, time.UTC)
	if perr != nil {
		res.Errors = append(res.Errors, "参数错误："+perr.Error())
		res.ExitCode = 2
		return res
	}
	if err := CheckForecastRange(start, end, e.now()); err != nil {
		res.Errors = append(res.Errors, "参数错误："+err.Error())
		res.ExitCode = 2
		return res
	}

	if p.Source == SourceTomorrow || p.Source == SourceMeteoblue {
		res.Errors = append(res.Errors,
			"日出模式仅支持 Open-Meteo（A 轨，需要气压层剖面反演云海几何），"+
				"--source tomorrow/meteoblue 在此模式下不可用")
		res.ExitCode = 2
		return res
	}

	// 日出模式不产出逐小时明细，也走不通抖音竖图（竖图按流星雨报告的章节名匹配）。
	// 这两项被要点名跳过时如实告知，绝不能默默吞掉用户的 --csv/--json/--douyin。
	if p.ExportCSV || p.ExportJSON || e.Cfg.Output.ExportCSV || e.Cfg.Output.ExportJSON {
		res.Warnings = append(res.Warnings,
			"日出模式不导出逐小时 CSV/JSON 明细（该明细是流星雨模式的逐夜逐时评级），"+
				"本次仅产出 Markdown 报告")
	}
	if p.Douyin {
		res.Warnings = append(res.Warnings,
			"日出模式暂不支持抖音竖图：竖图渲染按流星雨报告的章节名匹配，"+
				"日出报告章节不同，已跳过出图（Markdown 报告不受影响）")
	}

	// 放宽夜窗到含日出（NightEndHour→8），用配置副本，不改全局默认。
	cfg := e.Cfg
	if cfg.Window.NightEndHour < 8 {
		cfg.Window.NightEndHour = 8
	}

	sites, warns, err := e.resolveSites(p)
	res.Warnings = append(res.Warnings, warns...)
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
		res.ExitCode = 2
		return res
	}
	if len(sites) == 0 {
		res.Errors = append(res.Errors, "没有任何启用的观测点位")
		res.ExitCode = 2
		return res
	}
	res.Sites = sites

	models := p.Models
	if models == "" {
		models = e.Cfg.API.Models
	}
	explicitModels := p.Models != ""
	arriveBufferMin := e.Cfg.Window.ArriveBufferMin

	client := e.Client
	if client == nil {
		client = api.New(e.Cfg.API, !p.NoCache, api.WithLogger(e.logf))
	}

	outDir := p.OutDir
	if outDir == "" {
		outDir = e.Cfg.Output.OutDir
	}
	if outDir == "" {
		outDir = "reports"
	}

	repOffsetHours := 8.0
	for _, site := range sites {
		if ctx.Err() != nil {
			res.Errors = append(res.Errors, "执行被取消："+ctx.Err().Error())
			res.ExitCode = 1
			return res
		}
		siteModels := models
		if !explicitModels {
			siteModels = api.ResolveModel(site.Region, models)
		}
		resp, _, ferr := client.FetchSite(ctx, site, start, end, siteModels)
		if ferr != nil {
			res.Warnings = append(res.Warnings,
				fmt.Sprintf("[%s] 获取/解析失败：%v", site.Name, ferr))
			continue
		}
		utcOffsetHours := float64(resp.UTCOffsetSeconds) / 3600.0
		if utcOffsetHours != 0 {
			repOffsetHours = utcOffsetHours
		}
		r := BuildSunriseReport(site, resp, targetNight, sunriseDate, cfg, resp.UTCOffsetSeconds, arriveBufferMin)
		res.Sunrise = append(res.Sunrise, r)
		e.logf("[%s] 日出模式聚合：云海 %dh / 朝霞 %s / 可信度 %s",
			site.Name, r.CloudSeaHours, r.DawnGlow, r.Confidence)
	}

	if len(res.Sunrise) == 0 {
		res.Errors = append(res.Errors,
			"所有点位均无有效数据。请检查：网络是否可达 api.open-meteo.com、"+
				"日期是否在预报范围内、--models 是否拼写正确。")
		e.emitSunriseReport(&res, p, outDir)
		res.ExitCode = 1
		return res
	}

	meta := ReportMeta{
		Mode:           "sunrise",
		Models:         models,
		Start:          p.SunriseDate,
		End:            p.SunriseDate,
		Nights:         nights,
		NightsDesc:     nightsDesc,
		Timezone:       e.Cfg.API.Timezone,
		UTCOffsetHours: repOffsetHours,
		Sites:          sites,
		GeneratedAt:    e.now().Format("2006-01-02 15:04:05"),
		Source:         report.MetaSourceOpenMeteo,
		Disclaimer:     "云海几何由气压层剖面反演；天文量为纯 Go 近似算法结果，均非观测实测值。",
	}
	if meta.Timezone == "" {
		meta.Timezone = "Asia/Shanghai"
	}
	res.Meta = meta

	if !p.Quiet {
		w := p.Stdout
		if w == nil {
			w = e.Stdout
		}
		if w == nil {
			w = os.Stdout
		}
		report.PrintSunriseReport(w, res.Sunrise, meta, e.Cfg)
	}

	e.emitSunriseReport(&res, p, outDir)
	res.ExitCode = 0
	return res
}

// emitSunriseReport 生成日出模式 Markdown 报告并把路径写回 res。
// 与流星雨报告 emitReport 等价，但调用 WriteSunriseMarkdownReport。
func (e *Engine) emitSunriseReport(res *ExecResult, p RunParams, outDir string) {
	if p.NoReport {
		e.logf("已指定 --no-report，跳过 Markdown 报告生成")
		return
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("Markdown 报告生成失败：创建目录 %s 失败：%v", outDir, err))
		return
	}
	path, err := report.WriteSunriseMarkdownReport(res.Sunrise, res.Meta, e.Cfg, outDir)
	if err != nil {
		res.Warnings = append(res.Warnings, "Markdown 报告生成失败："+err.Error())
		return
	}
	res.ReportPath = path
	e.logf("已生成 Markdown 报告：%s", path)
}

// sortRows 按时间、站点名稳定排序，保证同一批输入每次产出的报告顺序一致。
func sortRows(rows []HourRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].TimeISO != rows[j].TimeISO {
			return rows[i].TimeISO < rows[j].TimeISO
		}
		return rows[i].Site < rows[j].Site
	})
}

// nightKeys 提取实际出现过的夜 ID，去重并按字典序（即时间序）排列。
func nightKeys(rows []HourRow) []string {
	seen := make(map[string]bool, 8)
	out := make([]string, 0, 8)
	for i := range rows {
		if !seen[rows[i].Night] {
			seen[rows[i].Night] = true
			out = append(out, rows[i].Night)
		}
	}
	sort.Strings(out)
	return out
}

// rowsOfNight 筛出属于指定夜的行，保持原有顺序。
func rowsOfNight(rows []HourRow, night string) []HourRow {
	out := make([]HourRow, 0, len(rows))
	for i := range rows {
		if rows[i].Night == night {
			out = append(out, rows[i])
		}
	}
	return out
}
