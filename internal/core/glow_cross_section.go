package core

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

// 大气截面几何判定：对齐 sunsetbot 的「沿太阳方向 ~800km 大气截面 + AOD 等效云底 +
// 几何判阳光能否照射云底」思路。与旧 sunset 口径（仅 GFS/ECMWF 取低 + AOD 降级 +
// 本地点太阳高度门）相比，多了「方向性」与「沿截面多点取数」两个维度：
// 辉光云必须在太阳方向上有、且沿光路不被气溶胶消光沉入光学地面。

const (
	// aodOpticalGroundPerUnit 是 CAMS AOD 对「等效光学地面高度」的抬升系数（米/单位 AOD）。
	// 标定自 sunsetbot 公开深圳案例：AOD=0.15 把 2000m 云底等效压到 750m 的光学地面
	// → 等效光学地面抬升约 1250m → 系数≈5000。AOD 越高，等效光学地面越高，
	// 云底相对它的 illuminable 高度越低，越难被曙/暮光照射（这正是 sunsetbot 判「无晚霞」的机制）。
	aodOpticalGroundPerUnit = 5000.0
	// crossSectionCloudCoverMin 判定「有效云载体」的最低云量（%），低于此视为无云载体。
	// 与 sunset 口径的朝霞「小烧」门（聚合中高云量 midhigh ≥ 5%）对齐——
	// 取气压层廓线里「最低一个云量≥此阈值」的层作为云载体。
	// 不再用旧版更严的 20% 连续整层门槛（后者依赖 DetectLayers 的云海口径阈值），
	// 否则会让「聚合中云仅 5%」的点位被几何门从「小烧」误压成「无」，与 sunset 口径自相矛盾。
	// AOD 气溶胶遮挡造成的下压（sunsetbot 真正核心机制）这一层仍保留。
	crossSectionCloudCoverMin = 5.0
	// shadowConeMaxDeg 是仍认为云可被曙/暮光照射的最大太阳俯角（度）。
	// 太阳俯角超过此值（太深，落入地球本影锥之外）的云不计入辉光可达。
	// 该阈值本身很宽松（800km 内云层的几何所需俯角通常 <1°），
	// 真正的判别权重在「有无云」与「AOD 是否把等效云底沉入光学地面」两项。
	shadowConeMaxDeg = 18.0
)

// CrossSectionPoint 是沿太阳方位角大气截面的一个采样点。
type CrossSectionPoint struct {
	DistKm       float64 // 距机位水平距离（km）
	Lat, Lon     float64
	CloudBaseMSL float64 // 该点最低有效云层云底海拔（m）；无云则为负
	HasCloud     bool
	MaxCC        float64 // 该云层最大云量（%）
	AOD          float64 // CAMS AOD；缺测为负
	HasAOD       bool
}

// destPoint 沿大圆从 (lat,lon) 出发，按方位角 azDeg（自正北顺时针）、距离 distKm 到达的点。
// azDeg 约定与 astro.Compute().SunAzimuth 一致（自正北顺时针），可直接喂入。
func destPoint(lat, lon, distKm, azDeg float64) (float64, float64) {
	const R = 6371.008 // 地球平均半径 km
	latR := lat * math.Pi / 180
	lonR := lon * math.Pi / 180
	azR := azDeg * math.Pi / 180
	ang := distKm / R
	lat2 := math.Asin(math.Sin(latR)*math.Cos(ang) + math.Cos(latR)*math.Sin(ang)*math.Cos(azR))
	lon2 := lonR + math.Atan2(math.Sin(azR)*math.Sin(ang)*math.Cos(latR),
		math.Cos(ang)-math.Sin(latR)*math.Sin(lat2))
	return lat2 * 180 / math.Pi, lon2 * 180 / math.Pi
}

// BuildCrossSection 沿太阳方位角 azDeg 从机位向外采样 distances(km)，每点取 GFS 云廓线 + CAMS AOD，
// 反演最低有效云层云底。evalTime 决定取哪个时次的廓线（取时间轴最近者）。
//
// 每点一次 FetchSite(gfs_seamless) + 一次 FetchAOD，采样点越多取数越重——
// 故仅 --glow-cross-section 开启时调用，默认报告不触发。
func BuildCrossSection(ctx context.Context, client *api.Client, site model.Site,
	start, end time.Time, evalTime time.Time, azDeg float64, distances []float64,
	cfg *config.Config) ([]CrossSectionPoint, error) {

	pts := make([]CrossSectionPoint, 0, len(distances))
	for _, d := range distances {
		lat, lon := destPoint(site.Lat, site.Lon, d, azDeg)
		sub := model.Site{
			Name:     fmt.Sprintf("%s@%.0fkm", site.Name, d),
			Lat:      lat,
			Lon:      lon,
			Alt:      site.Alt,
			Region:   site.Region,
			Timezone: site.Timezone,
		}
		resp, _, err := client.FetchSite(ctx, sub, start, end, "gfs_seamless")
		if err != nil {
			return nil, fmt.Errorf("截面点 %.0fkm 取数失败：%w", d, err)
		}
		idx := nearestIndex(resp.Times, evalTime)
		levels := profile.BuildProfile(resp.LevelValues(idx), cfg.Thresh)
		p := CrossSectionPoint{DistKm: d, Lat: lat, Lon: lon}
		if l, ok := lowestCloudLevel(levels); ok {
			p.HasCloud = true
			p.CloudBaseMSL = l.Height
			p.MaxCC = l.CCV()
		} else {
			p.CloudBaseMSL = -1
		}
		// AOD 单点取数失败不致命：缺测时按无 AOD 修正（optGround=0）处理，记警告由调用方兜底。
		if am, aerr := client.FetchAOD(ctx, sub, start, end); aerr == nil {
			if v, ok := am[wallClockUTC(evalTime)]; ok && v.Valid {
				p.AOD = v.V
				p.HasAOD = true
			}
		}
		pts = append(pts, p)
	}
	return pts, nil
}

func nearestIndex(times []time.Time, t time.Time) int {
	best, bestD := 0, math.MaxFloat64
	for i, tm := range times {
		d := math.Abs(tm.Sub(t).Seconds())
		if d < bestD {
			bestD, best = d, i
		}
	}
	return best
}

// lowestCloudLevel 从按高度升序的廓线中取「最低一个云量≥ crossSectionCloudCoverMin 的层」
// 作为云载体。阈值 5% 与 sunset 口径的「薄高云≥5% 即小烧」对齐，
// 不再要求 DetectLayers 那种连续≥20% 的整层云——避免几何门比 sunset 口径更严、把
// 「聚合中云仅 5%」的点位误压成「无」。返回该层（取其海拔作等效云底、云量作 MaxCC）与是否找到。
func lowestCloudLevel(levels []profile.Level) (profile.Level, bool) {
	for _, l := range levels { // levels 已按高度升序，首个命中即最低
		if l.CC.Valid && l.CC.V >= crossSectionCloudCoverMin {
			return l, true
		}
	}
	return profile.Level{}, false
}

// RequiredElevDeg 返回距机位 dKm、等效云底海拔 hMSL 的云，被地球本影锥照亮所需的
// 太阳俯角（度，取正）：h/D = tan(θ)。θ 越小越容易被照亮。
func RequiredElevDeg(hMSL, dKm float64) float64 {
	d := dKm * 1000.0
	if d <= 0 {
		return 0
	}
	return math.Atan2(hMSL, d) * 180 / math.Pi
}

// AssessCrossSection 判断沿截面的云能否被曙/暮光照射（等效 sunsetbot 的几何判定）。
// 任一点满足「有有效云层 + AOD 修正后等效云底>0 + 所需俯角≤本影锥上限」即视为可达（lit）。
// 返回 lit、可达点数、人话原因（用于报告透明化）。
//
// 设计取舍：几何所需俯角对 800km 内云层通常 <1°，远小于 shadowConeMaxDeg(18°)，
// 故真正的判别权重落在「有无云」与「AOD 等效云底是否沉入光学地面」两项——
// 这与 sunsetbot「有云且不被气溶胶遮挡即可染红」的核心一致。
func AssessCrossSection(pts []CrossSectionPoint) (lit bool, litPoints int, reason string) {
	var noCloud, blockedByAOD, tooFar int
	for _, p := range pts {
		if !p.HasCloud {
			noCloud++
			continue
		}
		optGround := 0.0
		if p.HasAOD {
			optGround = aodOpticalGroundPerUnit * p.AOD
		}
		illum := p.CloudBaseMSL - optGround // AOD 修正后的等效可照射高度
		if illum <= 0 {
			blockedByAOD++
			continue
		}
		req := RequiredElevDeg(illum, p.DistKm)
		if req <= shadowConeMaxDeg {
			litPoints++
		} else {
			tooFar++
		}
	}
	if litPoints > 0 {
		return true, litPoints,
			fmt.Sprintf("大气截面几何判定：沿太阳方向 %d/%d 个采样点云层可被曙/暮光照射（AOD 未将其沉入光学地面）",
				litPoints, len(pts))
	}
	switch {
	case noCloud == len(pts):
		return false, 0, "大气截面几何判定：沿太阳方向全程无有效云层（无可染红云载体）"
	case blockedByAOD == len(pts)-noCloud:
		return false, 0, "大气截面几何判定：沿太阳方向云层均被 CAMS AOD 气溶胶消光遮挡（等效云底沉入光学地面）"
	default:
		return false, 0, "大气截面几何判定：沿太阳方向云层超出本影锥可达范围（过远/过高）"
	}
}
