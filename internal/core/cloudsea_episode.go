package core

import (
	"sort"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/profile"
	"github.com/prophetcro/astro-mountain/internal/report"
)

// 逐小时云海状态。缺测与「有数据但无云海」必须分开：
// 两者都意味着这一小时不能计入云海时长，但缺测绝不等于云海散了，
// 不该把一段连续云海切断（2026-09 修复）。
const (
	snapNoSea   = 0 // 廓线可用、判定无云海
	snapSea     = 1 // 判定有云海
	snapMissing = 2 // 廓线缺测，状态未知
)

// hourSnap 某一小时的云海判定快照，供 mergeEpisodes 合并成时段。
type hourSnap struct {
	t       time.Time
	state   int
	topMSL  float64
	topAGL  float64
	thick   float64
	submrgd bool
}

// CollectCloudSeaEpisodesForNight 抽取某站点某观测夜的全部云海时段。
//
// 与 AnalyseSite 并行计算（独立重新跑一遍 BuildProfile/DetectLayers），
// 保持现有 HourRow 接口不被改。BuildProfile 是纯函数 + 无网络，重复计算开销可忽略。
//
// targetNight 用 NightIDOf 的口径（YYYY-MM-DD，回拨 12h 后取日期）。
// 只有落在夜间窗口内的小时参与计算，确保结果与报告展示的「夜」一致。
//
// 云海是否成立一律由 profile.ClassifySeaGeometry 裁决——
// 与 profile.EvaluateHour（逐小时评级）、AnalyseSite（「云海 有/无」列）
// 共用同一份几何判定，杜绝同一份廓线被三处判出不同结论。
//
// 返回的时段结构为 report.CloudSeaEpisode（类型定义在 report 包，避免循环依赖）。
func CollectCloudSeaEpisodesForNight(site Site, resp *api.Response,
	targetNight string, cfg config.Config) []report.CloudSeaEpisode {

	step := seriesInterval(resp.Times)
	siteAlt := site.Alt

	var snaps []hourSnap

	for idx, localDT := range resp.Times {
		if !InNightWindow(localDT.Hour(), cfg.Window) {
			continue
		}
		if NightIDOf(localDT) != targetNight {
			continue
		}

		levelValues := resp.LevelValues(idx)
		levels := profile.BuildProfile(levelValues, cfg.Thresh)
		if !ProfileUsable(levels) {
			// 廓线缺测：状态未知，记为 missing 而不是直接丢弃。
			// 若它处在两段云海之间，合并阶段会把它并入时段并如实标注，
			// 而不是像「无云海」那样把连续云海切断。
			snaps = append(snaps, hourSnap{t: localDT, state: snapMissing})
			continue
		}
		layers := profile.DetectLayers(levels, cfg.Thresh)

		g := profile.ClassifySeaGeometry(siteAlt, layers, cfg.Thresh)
		if !g.Present {
			snaps = append(snaps, hourSnap{t: localDT, state: snapNoSea})
			continue
		}
		// 与 AnalyseSite 一致：低云量够才认作云面，过滤零散积云。
		surface := resp.Surface(idx)
		low := surface.CloudCoverLow
		if !low.Valid {
			low = profile.MaxCCBelow(levels, 2500.0)
		}
		if !low.Valid || low.V < cloudSeaDeckLowCC {
			snaps = append(snaps, hourSnap{t: localDT, state: snapNoSea})
			continue
		}

		// 取时段内最厚的云海层厚度（脚下的层都是云海的一部分，取 max 更直观）。
		// 淹没型脚下没有独立层，g.Thickness 就是包裹机位那层的厚度，作为起点。
		thick := g.Thickness
		for _, lv := range layers {
			if lv.TopMSL < siteAlt && lv.Thickness() > thick {
				thick = lv.Thickness()
			}
		}

		snaps = append(snaps, hourSnap{
			t:       localDT,
			state:   snapSea,
			topMSL:  g.TopMSL,
			topAGL:  g.TopAGL,
			thick:   thick,
			submrgd: g.Kind == profile.SEA_SUBMERGED,
		})
	}

	if len(snaps) == 0 {
		return nil
	}

	// snaps 来自按时间遍历的 resp.Times，本就升序；显式排序防御性地保证。
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].t.Before(snaps[j].t) })

	return mergeEpisodes(snaps, step)
}

// mergeEpisodes 把逐小时快照合并成连续时段。
//
// step 是序列的实际时间间隔（由 resp.Times 推导，通常是 1h）。
// 合并规则（2026-09 重写，修掉两个缺陷）：
//  1. 相邻间隔 == step 视为连续并合并；间隔 > step*1.5 才切分。
//     step 由真实数据推导，不再硬编码 1h——3h 分辨率的模式不会再把
//     一段 9h 的云海切成 3 段各 1h。
//  2. End 取「最后一个有云海时次的结束时刻」(lastSea + step)，
//     与 HoursCount 一致，不会像早期版本那样少算一小时。
//  3. 缺测时次（state=snapMissing）不切段、不计入 HoursCount；
//     只有夹在两个有云海时次之间的缺测才被吸收，并记进 MissingHours
//     由渲染层如实标注「含 N 时次缺测」。既不假装知道，也不假装云海散了。
func mergeEpisodes(snaps []hourSnap, step time.Duration) []report.CloudSeaEpisode {
	if step <= 0 {
		step = time.Hour
	}
	// 允许的连续间隔：恰好一个 step，再留半个 step 的浮点/时区容差。
	maxGap := step + step/2

	eps := make([]report.CloudSeaEpisode, 0, 2)
	var cur *report.CloudSeaEpisode
	// lastT 是上一个**已处理**快照的时刻（三种状态都算）。
	// 间隔连续性必须以它为锚：若用「上一个有云海时次」当锚，
	// 中间夹的缺测时次会让间隔凭空多出 1 个 step，把连续云海误切成两段
	// ——那正是缺测容错想修掉的问题。
	lastT := time.Time{}
	pendingMissing := 0 // 已遇到但尚未确认「夹在云海中间」的缺测时次

	flush := func() {
		if cur != nil {
			eps = append(eps, *cur)
			cur = nil
		}
		pendingMissing = 0
	}

	for _, s := range snaps {
		// 段已存在、且距上一个快照超过一个 step（含半个 step 容差）→ 断开。
		// 缺测时次不参与切段（下面单独处理），只用它推进 lastT。
		contiguous := cur != nil && (!lastT.IsZero() && s.t.Sub(lastT) <= maxGap)

		switch s.state {
		case snapSea:
			if !contiguous {
				flush()
				cur = &report.CloudSeaEpisode{
					Start:         s.t,
					End:           s.t.Add(step),
					TopMSL:        s.topMSL,
					TopAGL:        s.topAGL,
					Submerged:     s.submrgd,
					Kind:          kindOf(s.submrgd),
					HoursCount:    1,
					PeakThickness: s.thick,
				}
				lastT = s.t
				continue
			}
			// 连续：把此前挂起的缺测时次正式并入本段并计入标注。
			cur.MissingHours += pendingMissing
			pendingMissing = 0
			cur.End = s.t.Add(step)
			cur.HoursCount++
			lastT = s.t
			if s.topMSL > cur.TopMSL {
				cur.TopMSL = s.topMSL
				cur.TopAGL = s.topAGL
			}
			if s.submrgd {
				cur.Submerged = true
				cur.Kind = profile.SEA_SUBMERGED
			}
			if s.thick > cur.PeakThickness {
				cur.PeakThickness = s.thick
			}

		case snapMissing:
			// 段内遇到缺测：挂起，等下一个有云海时次来确认它确实是「夹在中间」。
			// 段外（还没开始）或段尾的缺测都不吸收——
			// 尾部缺测我们无从得知云海是否延续，不延长时间才是诚实的。
			if cur != nil {
				pendingMissing++
			}
			lastT = s.t

		default: // snapNoSea：确有数据、确无云海，是真正的断点。
			flush()
			lastT = s.t
		}
	}
	flush()

	return eps
}

// kindOf 把「本小时是否淹没机位」映射成 CloudSeaEpisode.Kind。
func kindOf(submerged bool) string {
	if submerged {
		return profile.SEA_SUBMERGED
	}
	return profile.SEA_BELOW
}

// seriesInterval 从时间序列推导实际采样间隔（相邻时刻的最小正间隔）。
//
// 云海时段的合并与结束时刻必须按真实间隔算，不能假定恒为 1h：
// 部分模式/归档接口会给出 3h 分辨率，硬编码 1h 会把一段 9h 的云海
// 切成 3 段各 1h，末段结束时刻还少算 2h。
func seriesInterval(times []time.Time) time.Duration {
	if len(times) < 2 {
		return time.Hour
	}
	best := time.Duration(0)
	for i := 1; i < len(times); i++ {
		d := times[i].Sub(times[i-1])
		if d > 0 && (best == 0 || d < best) {
			best = d
		}
	}
	if best <= 0 {
		return time.Hour
	}
	return best
}
