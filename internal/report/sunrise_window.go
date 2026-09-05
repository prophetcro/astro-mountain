package report

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 本文件实现「日出拍摄窗口」判定：云海有了、但半夜就散了，对日出云海摄影等于没有。
//
// 起因是用户实测反馈：汇总表只给「云海时长h」不给「云海时段」，自动推荐又纯按时长排序，
// 于是「半夜 2 小时云海、日出早散了」的点位被推成最优日出机位。
// 修法：把云海时段与「日出窗云海」档位写进汇总表，并让推荐排序优先看日出窗覆盖。
//
// 窗口长度取自配置 window.sunrise_window_before_min / window.sunrise_window_after_min
// （默认日出前 45min ~ 日出后 30min），配置未填时回落下面的内置默认值。
//
// ⚠️ 时区口径（比窗口长度更容易踩）：日出时刻与云海时段**承载时区不同**——
//   - SunriseTime 由 astro.SunriseTime 算出，挂在 FixedZone("local", utcOffsetSec) 上，
//     是「当地墙钟」（06:03 +0800，其绝对瞬间是前一日 22:03Z）；
//   - Episodes 的 Start/End 来自 api.Response.Times，是「把 UTC 偏移加进去之后
//     用 UTC 承载的当地墙钟」（当地 06:03 记作 06:03Z）。
//
// 两者直接相减会比真实墙钟差整整一个时区偏移（本项目 UTC+8，即 8 小时），
// 于是「日出 05:41、云海 05:00–06:00」会被算成八小时之外而判成「完全错开」。
// 因此本文件所有比较都先经 wallClockUTC 剥时区、只比墙钟，
// 与 core.wallClockUTC（朝霞取云量、近地雾取窗口）保持同一口径。

// 日出拍摄窗口的默认余量（分钟）。配置值 <= 0（未填/填错）时用它兜底，
// 保证任何调用方都拿得到一个有界的窗口，不会退化成「以日出为点的零长区间」。
const (
	defaultSunriseWindowBeforeMin = 45
	defaultSunriseWindowAfterMin  = 30
)

// 「日出窗云海」五档标签。
const (
	sunriseWindowFull    = "完整覆盖" // 云海整段盖住拍摄窗口
	sunriseWindowSunrise = "覆盖日出" // 日出瞬间脚下有云海，但没盖满整个窗口
	sunriseWindowPartial = "部分重叠" // 云海碰到窗口，但日出那一刻没有
	sunriseWindowMissed  = "完全错开" // 有云海，可惜和拍摄窗口一点不沾
	sunriseWindowNone    = "无云海"  // 该夜根本没检出云海
)

// 「日出窗云海」档位序，数值越大越值得去；用于推荐排序的高位权重。
const (
	sunriseWindowRankFull    = 4
	sunriseWindowRankSunrise = 3
	sunriseWindowRankPartial = 2
	sunriseWindowRankMissed  = 1
	sunriseWindowRankNone    = 0
)

// cloudSeaTimeSegMax 汇总表「云海时段」列最多展示的时段数。
// 列宽有限，超出部分用「…」收尾，完整时段在各点位明细表里已有。
const cloudSeaTimeSegMax = 2

// wallClockUTC 把时刻剥去时区、只保留墙钟（年月日时分秒），统一用 UTC 承载。
//
// report 不可反向依赖 core（core 已 import report），故在此与 core.wallClockUTC
// 同口径重实现一份：比较前先剥时区，只比墙钟。时区混用会整体平移一个时区偏移，
// 是「云海明明盖住日出却被判完全错开」的根源。
func wallClockUTC(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// episodeWallSpan 返回云海段的首尾墙钟（已剥时区、UTC 承载），供所有比较使用。
func episodeWallSpan(ep CloudSeaEpisode) (start, end time.Time) {
	return wallClockUTC(ep.Start), wallClockUTC(ep.End)
}

// sunriseCaptureWindow 给出该站点的日出拍摄窗口 [start, end)：
// [日出 − beforeMin, 日出 + afterMin]。beforeMin/afterMin <= 0 时回落默认值。
//
// 起点是闭的（日出那一刻一定算在窗口里），终点按左闭右开与云海段同口径比较。
// 返回值是「剥掉时区的墙钟」（UTC 承载），调用方须拿它和 episodeWallSpan 的输出比。
func sunriseCaptureWindow(r SunriseSiteResult, beforeMin, afterMin int) (start, end time.Time) {
	if beforeMin <= 0 {
		beforeMin = defaultSunriseWindowBeforeMin
	}
	if afterMin <= 0 {
		afterMin = defaultSunriseWindowAfterMin
	}
	sunrise := wallClockUTC(r.SunriseTime)
	return sunrise.Add(-time.Duration(beforeMin) * time.Minute),
		sunrise.Add(time.Duration(afterMin) * time.Minute)
}

// cloudSeaTimeLabel 把该夜各段云海压成紧凑时段串，供汇总表「云海时段」列使用。
//
// 单段："04:00–06:00"；多段："22:00–23:00, 00:00–01:00"（段间逗号+空格）。
// 超过 cloudSeaTimeSegMax 段时只列前两段并以「…」收尾；无云海返回空串，
// 由调用方的 orDash 渲染成「-」。
func cloudSeaTimeLabel(r SunriseSiteResult) string {
	if len(r.Episodes) == 0 {
		return ""
	}
	segs := make([]string, 0, cloudSeaTimeSegMax+1)
	for i, ep := range r.Episodes {
		if i >= cloudSeaTimeSegMax {
			segs = append(segs, "…")
			break
		}
		segs = append(segs, ep.Start.Format("15:04")+"–"+ep.End.Format("15:04"))
	}
	return strings.Join(segs, ", ")
}

// sunriseWindowLabel 判定云海与日出拍摄窗口的关系，返回五档标签之一。
//
// 判定优先级（先满窗、再日出瞬间、再部分重叠，最后才判错开）：
//  1. 无云海 → 无云海
//  2. 各段云海的并集完整盖住 [winStart, winEnd) → 完整覆盖
//  3. 日出瞬间脚下有云海（SunriseTime 落在某段内）→ 覆盖日出
//  4. 有云海段与窗口相交、但不覆盖日出瞬间 → 部分重叠
//  5. 有云海段、但一段都不与窗口相交 → 完全错开
//
// 端点语义：云海段按**左闭右开 [Start, End)** 处理——End 是消散时刻，不是最后一刻有云海
// （依据见 episodeContains）。把 End 当闭区间会让「云海恰好在窗口起点前消散」的点位
// 被判成部分重叠，用户照着开上山却什么也没拍到。
func sunriseWindowLabel(r SunriseSiteResult, beforeMin, afterMin int) string {
	if len(r.Episodes) == 0 {
		return sunriseWindowNone
	}
	winStart, winEnd := sunriseCaptureWindow(r, beforeMin, afterMin)
	if cloudSeaCoversWindow(r.Episodes, winStart, winEnd) {
		return sunriseWindowFull
	}

	overlapped := false
	for _, ep := range r.Episodes {
		if !episodeOverlapsWindow(ep, winStart, winEnd) {
			continue
		}
		overlapped = true
		if episodeContains(ep, r.SunriseTime) {
			return sunriseWindowSunrise
		}
	}
	if overlapped {
		return sunriseWindowPartial
	}
	return sunriseWindowMissed
}

// sunriseWindowRank 把「日出窗云海」标签换成可排序的档位序（无匹配按 0 处理）。
func sunriseWindowRank(r SunriseSiteResult, beforeMin, afterMin int) int {
	switch sunriseWindowLabel(r, beforeMin, afterMin) {
	case sunriseWindowFull:
		return sunriseWindowRankFull
	case sunriseWindowSunrise:
		return sunriseWindowRankSunrise
	case sunriseWindowPartial:
		return sunriseWindowRankPartial
	case sunriseWindowMissed:
		return sunriseWindowRankMissed
	default:
		return sunriseWindowRankNone
	}
}

// episodeContains 判断时刻 t 是否落在云海段内。
//
// 区间语义是**左闭右开 [Start, End)**：End 是「消散时刻」而非「最后一刻有云海」——
// 由 core.mergeEpisodes 保证：End = 最后一个有云海时次 + step，
// 明细表表头也写作「出现 / 消散」，且 HoursCount 只数 [Start, End) 内的时次
// （实测：23:00→01:00 记 2h，即只有 23:00、00:00 两个时次有云海）。
// 因此 06:00 消散的云海在 06:00 那一刻已确认无云，不该算进窗口。
// 两侧都先剥时区，只比墙钟。
func episodeContains(ep CloudSeaEpisode, t time.Time) bool {
	start, end := episodeWallSpan(ep)
	tt := wallClockUTC(t)
	return !tt.Before(start) && tt.Before(end)
}

// episodeOverlapsWindow 判断云海段 [Start, End) 与窗口 [winStart, winEnd) 是否相交。
// 两端同按左闭右开处理：云海恰好在窗口起点消散（End == winStart）、
// 或窗口恰好在云海出现的那一刻结束，都算不相交——这是如实而非保守。
// 两侧都先剥时区，只比墙钟。
func episodeOverlapsWindow(ep CloudSeaEpisode, winStart, winEnd time.Time) bool {
	start, end := episodeWallSpan(ep)
	ws, we := wallClockUTC(winStart), wallClockUTC(winEnd)
	return start.Before(we) && end.After(ws)
}

// cloudSeaCoversWindow 判断各段云海 [Start, End) 的并集是否完整盖住 [winStart, winEnd)。
//
// 实现是「排序 + 扫一遍推进已覆盖上界」：一旦出现断档立即返回 false，
// 上界越过 winEnd 即返回 true。云海段之间的先后顺序不影响结果。
// 入参窗口与云海段都按墙钟、按左闭右开比较。
func cloudSeaCoversWindow(eps []CloudSeaEpisode, winStart, winEnd time.Time) bool {
	spans := make([]CloudSeaEpisode, len(eps))
	copy(spans, eps)
	sort.SliceStable(spans, func(i, j int) bool {
		return spans[i].Start.Before(spans[j].Start)
	})

	covered := wallClockUTC(winStart)
	target := wallClockUTC(winEnd)
	for _, sp := range spans {
		start, end := episodeWallSpan(sp)
		if !covered.Before(target) {
			return true
		}
		if !end.After(covered) {
			// 该段在已覆盖上界之前就已消散，对覆盖推进没有贡献。
			continue
		}
		if start.After(covered) {
			// 已覆盖上界与下一段之间出现空白，窗口盖不满。
			return false
		}
		covered = end
	}
	return !covered.Before(target)
}

// dawnGlowRank 给朝霞四档赋序，供综合推荐排序（无/空值 = 0）。
func dawnGlowRank(g string) int {
	switch g {
	case "大烧":
		return 3
	case "中烧":
		return 2
	case "小烧":
		return 1
	default:
		return 0
	}
}

// bestSunriseSite 给出综合最优机位名。
//
// 排序口径（高位优先）：日出窗云海档位 × 10000 + 朝霞档 × 1000 + 云海时长 × 10 + 可信度序。
// 之所以把「日出窗云海」放到最高位：日出云海摄影拍的是日出那一刻，
// 半夜两小时云海再长也拍不到，纯按时长排会把「云海早已散去」的点位推成最优。
// 时长与可信度只在同一档内做细分，保留原有的「长且可信」倾向。
func bestSunriseSite(results []SunriseSiteResult, beforeMin, afterMin int) string {
	best := ""
	bestScore := -1
	for _, r := range results {
		score := sunriseWindowRank(r, beforeMin, afterMin)*10000 +
			dawnGlowRank(r.DawnGlow)*1000 +
			r.CloudSeaHours*10 +
			confidenceRank(r.Confidence)
		if score > bestScore {
			bestScore = score
			best = r.Site
		}
	}
	return best
}

// sunriseSummaryHeaders 汇总表表头。单日与多日两条渲染路径共用一份，
// 避免两处各写一遍、日后改列时又漂移（这次「缺云海时间列」就是这么漏的）。
func sunriseSummaryHeaders() []string {
	return []string{"点位", "云海时段", "日出窗云海", "云海时长h",
		"云海形态", "朝霞", "近地雾", "云海可信度", "建议抵达", "结论"}
}

// sunriseSummaryRow 拼装汇总表的一行。
// 空值一律走 orDash 渲染成「-」，与表内其他可缺省列保持一致。
func sunriseSummaryRow(r SunriseSiteResult, beforeMin, afterMin int) []string {
	return []string{
		r.Site,
		orDash(cloudSeaTimeLabel(r)),
		sunriseWindowLabel(r, beforeMin, afterMin),
		fmt.Sprintf("%d", r.CloudSeaHours),
		orDash(r.CloudSeaForm),
		r.DawnGlow,
		orDash(fogPotentialCell(r.FogPotential)),
		r.Confidence,
		r.ArriveBy.Format("15:04"),
		r.Rating,
	}
}

// sunriseSummaryTable 渲染整张汇总表（表头 + 各行），供单日/多日两条路径共用。
func sunriseSummaryTable(results []SunriseSiteResult, beforeMin, afterMin int) []string {
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, sunriseSummaryRow(r, beforeMin, afterMin))
	}
	return MDTable(sunriseSummaryHeaders(), rows)
}
