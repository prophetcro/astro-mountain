package core

import (
	"sort"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/profile"
	"github.com/prophetcro/astro-mountain/internal/report"
)

// CollectCloudSeaEpisodesForNight 抽取某站点某观测夜的全部云海时段。
//
// 与 AnalyseSite 并行计算（独立重新跑一遍 BuildProfile/DetectLayers/HighestBeneath），
// 保持现有 HourRow 接口不被改。BuildProfile 是纯函数 + 无网络，重复计算开销可忽略。
//
// targetNight 用 NightIDOf 的口径（YYYY-MM-DD，回拨 12h 后取日期）。
// 只有落在夜间窗口内的小时参与计算，确保结果与报告展示的「夜」一致。
//
// 返回的时段结构为 report.CloudSeaEpisode（类型定义在 report 包，避免循环依赖）。
func CollectCloudSeaEpisodesForNight(site Site, resp *api.Response,
	targetNight string, cfg config.Config) []report.CloudSeaEpisode {

	siteAlt := site.Alt

	type hourSnap struct {
		t      time.Time
		hasSea bool
		topMSL float64
		thick  float64
	}

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
			continue
		}
		layers := profile.DetectLayers(levels, cfg.Thresh)

		top, ok := profile.HighestBeneath(siteAlt, layers)
		if !ok {
			continue
		}
		// 与 AnalyseSite 一致：低云量够才认作云面，过滤零散积云。
		surface := resp.Surface(idx)
		low := surface.CloudCoverLow
		if !low.Valid {
			low = profile.MaxCCBelow(levels, 2500.0)
		}
		if !low.Valid || low.V < cloudSeaDeckLowCC {
			continue
		}

		// 取时段内最厚的云海层厚度（脚下的层都是云海的一部分，取 max 更直观）。
		thick := 0.0
		for _, lv := range layers {
			if lv.TopMSL < siteAlt && lv.Thickness() > thick {
				thick = lv.Thickness()
			}
		}

		snaps = append(snaps, hourSnap{
			t:      localDT,
			hasSea: true,
			topMSL: top,
			thick:  thick,
		})
	}

	if len(snaps) == 0 {
		return nil
	}

	// snaps 来自按时间遍历的 resp.Times，本就升序；显式排序防御性地保证。
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].t.Before(snaps[j].t) })

	// 合并：相邻 1h 间隔视为连续时段；间隔 ≥ 2h 则切分。
	// End 取「最后一小时的结束时刻」(lastSnap + 1h)，这样「出现→消散」跨度 = 实际小时数，
	// 与 HoursCount 一致：1 小时时段显示 23:00→00:00（而非 23:00→23:00），
	// N 小时时段显示 Start→(lastSnap+1h)。gap 判定以 lastT（最后一小时起点）为锚，
	// 避免 End 加了 1h 后把 2h 真实间隔误判成连续。
	const maxGapHours = 1
	eps := make([]report.CloudSeaEpisode, 0, 2)
	cur := report.CloudSeaEpisode{
		Start:         snaps[0].t,
		End:           snaps[0].t.Add(time.Hour),
		TopMSL:        snaps[0].topMSL,
		TopAGL:        siteAlt - snaps[0].topMSL,
		Submerged:     snaps[0].topMSL > siteAlt,
		PeakThickness: snaps[0].thick,
		HoursCount:    1,
	}
	lastT := snaps[0].t
	for i := 1; i < len(snaps); i++ {
		s := snaps[i]
		gap := s.t.Sub(lastT).Hours()
		if gap > float64(maxGapHours)+0.1 {
			eps = append(eps, cur)
			cur = report.CloudSeaEpisode{
				Start:         s.t,
				End:           s.t.Add(time.Hour),
				TopMSL:        s.topMSL,
				TopAGL:        siteAlt - s.topMSL,
				Submerged:     s.topMSL > siteAlt,
				PeakThickness: s.thick,
				HoursCount:    1,
			}
			lastT = s.t
			continue
		}
		cur.End = s.t.Add(time.Hour)
		cur.HoursCount++
		lastT = s.t
		if s.topMSL > cur.TopMSL {
			cur.TopMSL = s.topMSL
			cur.TopAGL = siteAlt - s.topMSL
		}
		if s.topMSL > siteAlt {
			cur.Submerged = true
		}
		if s.thick > cur.PeakThickness {
			cur.PeakThickness = s.thick
		}
	}
	eps = append(eps, cur)
	return eps
}
