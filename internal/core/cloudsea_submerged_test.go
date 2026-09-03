package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

// cloudsea_submerged_test.go 锁死「淹没型云海」的检出行为，是 2026-09 修复的回归测试。
//
// 修复前：云海时段检测只用 profile.HighestBeneath（严格 `TopMSL < siteAlt`）判定，
// 而逐小时评级 profile.EvaluateHour 额外承认「机位埋在云顶附近、脚下是厚云海」
// 的高山云海形态（REL_SEA_BELOW_IN_CLOUD）。同一份廓线两套口径，
// 导致淹没型被系统性漏检——真实数据 20 站点 1620 个夜窗时次中漏掉 185 个，
// 比实际检出的 142 个还多（用户"用了这么久一次云海都没出现但实际上是有的"正源于此）。
// 同时 Submerged 字段因判定条件互斥恒为 false，是死代码。
//
// 修复后：三方（逐小时评级 / 云海时段检测 / 「云海 有/无」列）统一由
// profile.ClassifySeaGeometry 裁决，Submerged 得以复活且语义正确。

// subNIU 牛草山机位海拔（米）。
const subNIU = 1442.0

// subNight 探针用的观测夜（NightIDOf 口径）。
const subNight = "2026-09-15"

func subSite() Site {
	return Site{Name: "牛草山", Lat: 31.047, Lon: 116.259, Alt: subNIU}
}

// subGH 是各气压层的位势高（米，海拔），从 1000hPa 一直铺到 800hPa。
// 机位 1442m 落在 875hPa(1220m) 与 850hPa(1477m) 之间。
var subGH = map[int]float64{
	1000: 300, 975: 520, 950: 740, 925: 960,
	900: 983, 875: 1220, 850: 1477, 825: 1700, 800: 1996,
}

// subFlat 造一条 9 时次的常量序列（与 makeCloudSeaResp 的时间轴等长）。
func subFlat(v float64) []float64 {
	return []float64{v, v, v, v, v, v, v, v, v}
}

// subBuildResp 在 makeCloudSeaResp 的基础上重建全部气压层：
// ccOf 决定每层的云量序列（nil 表示该层不参与反演）。
// 未设置的层因 CC 与 RH 全缺测，会被 BuildProfile 直接丢弃。
func subBuildResp(t *testing.T, ccOf map[int][]float64) *api.Response {
	t.Helper()
	resp := makeCloudSeaResp(t)
	for p, gh := range subGH {
		ccName, _, rhName := api.LevelVarNames(p)
		if ccOf[p] == nil {
			delete(resp.Series, ccName)
			delete(resp.Series, rhName)
			continue
		}
		subSetLevel(t, resp, p, ccOf[p], gh)
	}
	return resp
}

// subSetLevel 把某气压层的 云量/位势高/相对湿度 三个序列写进 resp。
func subSetLevel(t *testing.T, resp *api.Response, p int, cc []float64, gh float64) {
	t.Helper()
	ccName, ghName, rhName := api.LevelVarNames(p)

	put := func(name string, vs []float64) {
		if len(resp.Times) != 0 && len(vs) != len(resp.Times) {
			t.Fatalf("变量 %s 长度 %d 与时间轴 %d 不一致", name, len(vs), len(resp.Times))
		}
		opts := make([]model.OptFloat, len(vs))
		for i, v := range vs {
			opts[i] = model.Num(v)
		}
		resp.Series[name] = opts
	}
	put(ccName, cc)
	put(ghName, subFlat(gh))
	put(rhName, subFlat(80))
}

// subDump 打印某一夜逐小时的「evaluate.go 口径」与「episode 口径」对照表。
func subDump(t *testing.T, tag string, resp *api.Response, cfg config.Config) {
	t.Helper()
	site := subSite()
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s：逐小时对照（机位 %.0fm）\n", tag, subNIU)
	fmt.Fprintf(&sb, "  %-6s %-22s %-24s %-22s %s\n",
		"时刻", "evaluate关系", "层(底/顶)m", "ClassifySeaGeometry", "低云量")
	for idx, dt := range resp.Times {
		if !InNightWindow(dt.Hour(), cfg.Window) || NightIDOf(dt) != subNight {
			continue
		}
		levels := profile.BuildProfile(resp.LevelValues(idx), cfg.Thresh)
		layers := profile.DetectLayers(levels, cfg.Thresh)
		ev := profile.EvaluateHour(site, resp.Surface(idx), layers, levels, cfg.Thresh)
		g := profile.ClassifySeaGeometry(subNIU, layers, cfg.Thresh)

		shape := "—"
		if len(layers) > 0 {
			shape = fmt.Sprintf("[%.0f/%.0f]", layers[0].BaseMSL, layers[0].TopMSL)
		}
		geo := fmt.Sprintf("present=%v/%s", g.Present, g.Kind)
		low := resp.Surface(idx).CloudCoverLow
		lowStr := "缺测"
		if low.Valid {
			lowStr = fmt.Sprintf("%.0f", low.V)
		}
		fmt.Fprintf(&sb, "  %-6s %-22s %-24s %-22s %s\n",
			dt.Format("15:04"), ev.Relation, shape, geo, lowStr)
	}
	t.Logf("\n%s", sb.String())
}

// ---------------------------------------------------------------------------
// 缺陷 1：Submerged 是死代码
// ---------------------------------------------------------------------------

// TestCloudSeaSubmergedFlagIsSet 扫一批云-机位形态，校验 Submerged 只在该置位时置位。
//
// 修复前 Submerged 恒为 false：snaps 只在 HighestBeneath ok=true 时构造，
// 而 ok=true ⇒ topMSL < siteAlt ⇒ `topMSL > siteAlt` 恒为假，判定条件互斥。
func TestCloudSeaSubmergedFlagIsSet(t *testing.T) {
	cfg := config.Default()

	cloud := subFlat(70)
	clear := subFlat(0)

	scenes := []struct {
		name          string
		ccOf          map[int][]float64
		wantSubmerged bool
	}{
		{"脚下薄云海(仅900hPa有云)", map[int][]float64{
			900: subFlat(60), 875: clear, 850: clear, 825: clear, 800: clear,
		}, false},
		{"厚云海(900+875有云)", map[int][]float64{
			900: cloud, 875: cloud, 850: clear, 825: clear, 800: clear,
		}, false},
		// 850hPa(1477m) 高于机位 1442m 一点点，云层包住机位 → 淹没型。
		{"云海顶到机位脚下(900+875+850有云)", map[int][]float64{
			900: cloud, 875: cloud, 850: subFlat(60), 825: clear, 800: clear,
		}, true},
		{"淹没机位(1000→850全有云)", map[int][]float64{
			1000: cloud, 975: cloud, 950: cloud, 925: cloud, 900: cloud, 875: cloud,
			850: subFlat(60), 825: clear, 800: clear,
		}, true},
		{"淹没更深(1000→825全有云)", map[int][]float64{
			1000: cloud, 975: cloud, 950: cloud, 925: cloud, 900: cloud, 875: cloud,
			850: cloud, 825: subFlat(60), 800: clear,
		}, true},
		// 整层全有云 → 云顶被剖面截断，机位埋在厚云深处，不算可拍云海。
		{"整层全有云(顶出剖面)", map[int][]float64{
			1000: cloud, 975: cloud, 950: cloud, 925: cloud, 900: cloud, 875: cloud,
			850: cloud, 825: cloud, 800: cloud,
		}, false},
	}

	total, submerged := 0, 0
	for _, sc := range scenes {
		resp := subBuildResp(t, sc.ccOf)
		eps := CollectCloudSeaEpisodesForNight(subSite(), resp, subNight, cfg)
		n := 0
		for _, e := range eps {
			if e.Submerged {
				n++
			}
			// 反例锁定：每个时段的 Submerged 都必须与场景预期一致。
			if e.Submerged != sc.wantSubmerged {
				t.Errorf("场景「%s」Submerged=%v，期望 %v", sc.name, e.Submerged, sc.wantSubmerged)
			}
		}
		total += len(eps)
		submerged += n
		t.Logf("场景「%s」→ %d 段，Submerged=true 的段数 = %d", sc.name, len(eps), n)
	}
	t.Logf("合计 %d 个时段，其中 Submerged=true 共 %d 个", total, submerged)

	// 关键回归点：Submerged 不再是死代码，淹没形态必须置位。
	if submerged == 0 {
		t.Fatalf("Submerged 恒为 false（死代码回归）：6 个场景共 %d 个时段无一置位，"+
			"淹没型云海将无法在报告里被标注", total)
	}
}

// ---------------------------------------------------------------------------
// 缺陷 2：淹没机位时漏检云海，与 evaluate.go 口径不一致
// ---------------------------------------------------------------------------

// TestCloudSeaSubmergedDetected_VsEvaluate 核心回归点：同一份廓线，
// evaluate.go 判为 REL_SEA_BELOW_IN_CLOUD（云海在脚下·机位在云中）时，
// CollectCloudSeaEpisodesForNight 必须检出对应的云海时段——两者口径必须一致。
//
// 修复前：evaluate 判 8/8 时次为淹没型云海，而时段检测返回 0 段。
func TestCloudSeaSubmergedDetected_VsEvaluate(t *testing.T) {
	site := subSite()

	// 分别用内置默认阈值（CloudSeaBeneathDepthM=200）与 configs/config.json
	// 的线上阈值（CloudSeaBeneathDepthM=600）各跑一遍，排除阈值取值影响结论。
	for _, beneath := range []float64{200, 600} {
		cfg := config.Default()
		cfg.Thresh.CloudSeaBeneathDepthM = beneath

		cloud := subFlat(70)
		resp := subBuildResp(t, map[int][]float64{
			1000: cloud, 975: cloud, 950: cloud, 925: cloud, 900: cloud, 875: cloud,
			850: subFlat(60), 825: subFlat(0), 800: subFlat(0),
		})
		subDump(t, fmt.Sprintf("整夜淹没机位 (CloudSeaBeneathDepthM=%.0f)", beneath), resp, cfg)

		// 1) evaluate.go 口径：夜窗内逐小时关系应为 REL_SEA_BELOW_IN_CLOUD。
		inCloud := 0
		for idx, dt := range resp.Times {
			if !InNightWindow(dt.Hour(), cfg.Window) || NightIDOf(dt) != subNight {
				continue
			}
			levels := profile.BuildProfile(resp.LevelValues(idx), cfg.Thresh)
			layers := profile.DetectLayers(levels, cfg.Thresh)
			ev := profile.EvaluateHour(site, resp.Surface(idx), layers, levels, cfg.Thresh)
			if ev.Relation != profile.REL_SEA_BELOW_IN_CLOUD {
				t.Fatalf("前置条件不成立：%s 的关系为 %q（note=%q），"+
					"本用例需 REL_SEA_BELOW_IN_CLOUD 才能检验口径一致性",
					dt.Format("15:04"), ev.Relation, ev.Note)
			}
			inCloud++
		}
		if inCloud == 0 {
			t.Fatal("夜窗内没有任何时次，用例无效")
		}

		// 2) episode 口径：必须检出同样多的云海时次，且标记为淹没型。
		eps := CollectCloudSeaEpisodesForNight(site, resp, subNight, cfg)
		gotHours := 0
		for _, e := range eps {
			gotHours += e.HoursCount
			if !e.Submerged {
				t.Errorf("埋云时次未标记 Submerged：%+v", e)
			}
			if e.TopAGL >= 0 {
				t.Errorf("淹没型的 TopAGL 应为负（云顶高于机位），实际 %.1f", e.TopAGL)
			}
		}
		if gotHours != inCloud {
			t.Errorf("口径不一致：evaluate.go 判定 %d 个时次为「云海在脚下(机位在云中)」，"+
				"但云海时段只检出 %d 小时", inCloud, gotHours)
		}
	}
}

// TestCloudSeaSubmergedDetected_PartialNight 更贴近线上：
// 前半夜机位被淹没、后半夜云顶落到脚下，与 evaluate.go 对齐应是整夜 8h 连续云海。
//
// 修复前：前半夜 4 小时被静默吞掉，只检出后半夜那段。
func TestCloudSeaSubmergedDetected_PartialNight(t *testing.T) {
	cfg := config.Default()
	cfg.Thresh.CloudSeaBeneathDepthM = 600 // configs/config.json 的线上取值
	site := subSite()

	// idx0..3 (23:00–02:00) 淹没：1000→850 全有云（单层）；
	// idx4..8 (03:00–07:00) 仅 900hPa 有云（云顶落到脚下）。
	// 注意 875/850 也必须跟着 sub 走，否则前半夜会被切成两层。
	early := []float64{70, 70, 70, 70, 0, 0, 0, 0, 0}
	resp := subBuildResp(t, map[int][]float64{
		1000: early, 975: early, 950: early, 925: early,
		900: []float64{70, 70, 70, 70, 60, 60, 60, 60, 60},
		875: early, 850: early,
		825: subFlat(0), 800: subFlat(0),
	})
	subDump(t, "混合夜(前半夜淹没/后半夜云顶落到脚下)", resp, cfg)

	// 先确认各时次在 evaluate.go 口径下的关系分类。
	relSubmerged, relBelow := 0, 0
	for idx, dt := range resp.Times {
		if !InNightWindow(dt.Hour(), cfg.Window) || NightIDOf(dt) != subNight {
			continue
		}
		levels := profile.BuildProfile(resp.LevelValues(idx), cfg.Thresh)
		layers := profile.DetectLayers(levels, cfg.Thresh)
		ev := profile.EvaluateHour(site, resp.Surface(idx), layers, levels, cfg.Thresh)
		t.Logf("  %s  relation=%s", dt.Format("15:04"), ev.Relation)
		switch ev.Relation {
		case profile.REL_SEA_BELOW_IN_CLOUD:
			relSubmerged++
		case profile.REL_SEA_BELOW:
			relBelow++
		}
	}
	t.Logf("evaluate 口径：淹没型 %d 时次 + 脚下型 %d 时次 = %d（夜窗共 8 时次）",
		relSubmerged, relBelow, relSubmerged+relBelow)

	eps := CollectCloudSeaEpisodesForNight(site, resp, subNight, cfg)
	gotHours := 0
	for _, e := range eps {
		gotHours += e.HoursCount
	}
	if gotHours != relSubmerged+relBelow {
		t.Errorf("口径不一致：evaluate.go 判定 %d 个时次为云海形态，"+
			"但云海时段只检出 %d 小时：%+v", relSubmerged+relBelow, gotHours, eps)
	}
	// 混合夜里只要有一小时是淹没型，整段就应标记 Submerged。
	if relSubmerged > 0 && (len(eps) == 0 || !eps[0].Submerged) {
		t.Errorf("前半夜 %d 个时次为淹没型，但时段未标记 Submerged：%+v", relSubmerged, eps)
	}
}
