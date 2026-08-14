package core

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// 双模型交叉对比固定拉取的两套全球模式：ICON（欧洲中心区域，华东表现偏乐观）
// 与 GFS（美国全球，常更保守）。两者结论一致才可信。
const (
	crossIconModel = "icon_seamless"
	crossGfsModel  = "gfs_seamless"
)

// fetchAnalyse 拉取指定模式的预报并跑单站点评估，返回逐小时行。
func (e *Engine) fetchAnalyse(ctx context.Context, client *api.Client, site Site,
	start, end time.Time, targetNights map[string]bool, models string) ([]HourRow, error) {
	resp, _, err := client.FetchSite(ctx, site, start, end, models)
	if err != nil {
		return nil, err
	}
	return AnalyseSite(site, resp, targetNights, e.Cfg), nil
}

// resolveCompareRows 给出某个站点在 ICON 与 GFS 两套模式下的逐小时评估行。
//
// 主模式若正好是 ICON/GFS 之一，则复用已算好的 primaryRows，避免重复取数；
// 缺哪侧就补拉哪侧。任一侧取数失败仅返回警告，不中断主流程——
// 双模型对比是增强项，绝不该因为第二次取数失败而废掉整份报告。
func (e *Engine) resolveCompareRows(ctx context.Context, client *api.Client, site Site,
	start, end time.Time, targetNights map[string]bool, primaryModels string,
	primaryRows []HourRow) (icon, gfs []HourRow, warns []string) {

	if primaryModels == crossIconModel {
		icon = primaryRows
	} else if primaryModels == crossGfsModel {
		gfs = primaryRows
	}

	if icon == nil {
		rows, err := e.fetchAnalyse(ctx, client, site, start, end, targetNights, crossIconModel)
		if err != nil {
			warns = append(warns,
				fmt.Sprintf("[%s] 双模型对比 ICON 取数失败，该站对比降级为单模型：%v", site.Name, err))
		} else {
			icon = rows
		}
	}
	if gfs == nil {
		rows, err := e.fetchAnalyse(ctx, client, site, start, end, targetNights, crossGfsModel)
		if err != nil {
			warns = append(warns,
				fmt.Sprintf("[%s] 双模型对比 GFS 取数失败，该站对比降级为单模型：%v", site.Name, err))
		} else {
			gfs = rows
		}
	}
	return icon, gfs, warns
}

// PairCompareRows 把 ICON 与 GFS 两套逐小时行按 (站点, 整点) 配对，
// 生成双模型交叉对比的逐小时结论。某侧缺测时该侧评级记为 RATING_NODATA。
func PairCompareRows(site string, iconRows, gfsRows []HourRow) []model.ModelCompareRow {
	indexByTime := func(rows []HourRow) map[string]HourRow {
		m := make(map[string]HourRow, len(rows))
		for _, r := range rows {
			if r.HasData {
				m[r.TimeISO] = r
			}
		}
		return m
	}
	iconM := indexByTime(iconRows)
	gfsM := indexByTime(gfsRows)

	times := make([]string, 0, len(iconM)+len(gfsM))
	seen := make(map[string]bool, len(times))
	add := func(t string) {
		if !seen[t] {
			seen[t] = true
			times = append(times, t)
		}
	}
	for t := range iconM {
		add(t)
	}
	for t := range gfsM {
		add(t)
	}
	sort.Strings(times)

	out := make([]model.ModelCompareRow, 0, len(times))
	for _, t := range times {
		ir, iok := iconM[t]
		gr, gok := gfsM[t]
		iconRating := model.RATING_NODATA
		if iok {
			iconRating = ir.Rating
		}
		gfsRating := model.RATING_NODATA
		if gok {
			gfsRating = gr.Rating
		}
		night, short := "", t
		hour := -1
		if iok {
			night, short, hour = ir.Night, ir.TimeShort, ir.Hour
		} else if gok {
			night, short, hour = gr.Night, gr.TimeShort, gr.Hour
		}
		out = append(out, model.ModelCompareRow{
			Site:       site,
			Night:      night,
			TimeISO:    t,
			TimeShort:  short,
			Hour:       hour,
			IconRating: iconRating,
			GfsRating:  gfsRating,
			Consensus:  model.ClassifyConsensus(iconRating, gfsRating),
		})
	}
	return out
}
