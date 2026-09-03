package profile

import (
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
)

// TestClassifySeaGeometry 锁死云海几何的统一判定口径。
//
// ClassifySeaGeometry 是「是否存在可拍云海」的唯一权威口径，由三方共用：
// 逐小时评级 EvaluateHour、云海时段检测 core.CollectCloudSeaEpisodesForNight、
// 逐小时「云海 有/无」列 core.AnalyseSite。
//
// 2026-09 之前三方各自实现（评级器承认三种形态、检测器只认一种），
// 导致淹没型云海被系统性漏检。这些用例锁死三种形态的边界，
// 任何一处想绕过统一口径都会在这里被拦下。
func TestClassifySeaGeometry(t *testing.T) {
	const siteAlt = 1442.0
	th := config.Default().Thresh

	cases := []struct {
		name      string
		layers    []CloudLayer
		wantPres  bool
		wantKind  string
		wantBelow bool // TopAGL 应为正（机下）
	}{
		{
			name:     "空层→无云海",
			layers:   nil,
			wantPres: false,
		},
		{
			name:      "脚下型：云顶明显低于机位",
			layers:    []CloudLayer{{BaseMSL: 900, TopMSL: 1062, MaxCC: 70}},
			wantPres:  true,
			wantKind:  SEA_BELOW,
			wantBelow: true,
		},
		{
			name:      "淹没型：云从山脚堆过机位、脚下无独立层",
			layers:    []CloudLayer{{BaseMSL: 300, TopMSL: 1551, MaxCC: 70}},
			wantPres:  true,
			wantKind:  SEA_SUBMERGED,
			wantBelow: false,
		},
		{
			name: "薄云顶型：脚下云海 + 头顶薄云",
			layers: []CloudLayer{
				{BaseMSL: 900, TopMSL: 1062, MaxCC: 70},
				{BaseMSL: 1500, TopMSL: 1650, MaxCC: 40},
			},
			wantPres:  true,
			wantKind:  SEA_BELOW,
			wantBelow: true,
		},
		{
			name: "头顶厚云压死：脚下有云海但头顶云太厚",
			layers: []CloudLayer{
				{BaseMSL: 900, TopMSL: 1062, MaxCC: 70},
				{BaseMSL: 1500, TopMSL: 2200, MaxCC: 90},
			},
			wantPres: false,
		},
		{
			name:     "纯埋云：脚下云不够厚，不算云海",
			layers:   []CloudLayer{{BaseMSL: 1380, TopMSL: 1500, MaxCC: 70}},
			wantPres: false,
		},
		{
			name:     "埋在厚云深处：头顶云过厚",
			layers:   []CloudLayer{{BaseMSL: 300, TopMSL: 2100, MaxCC: 100}},
			wantPres: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := ClassifySeaGeometry(siteAlt, c.layers, th)
			if g.Present != c.wantPres {
				t.Fatalf("Present = %v，期望 %v（kind=%q）", g.Present, c.wantPres, g.Kind)
			}
			if !g.Present {
				if g.Kind != SEA_NONE {
					t.Errorf("无云海时 Kind 应为 %q，实际 %q", SEA_NONE, g.Kind)
				}
				return
			}
			if g.Kind != c.wantKind {
				t.Errorf("Kind = %q，期望 %q", g.Kind, c.wantKind)
			}
			if c.wantBelow && g.TopAGL <= 0 {
				t.Errorf("脚下型 TopAGL 应为正（机下 Xm），实际 %.1f", g.TopAGL)
			}
			if !c.wantBelow && g.TopAGL >= 0 {
				t.Errorf("淹没型 TopAGL 应为负（云顶高于机位），实际 %.1f", g.TopAGL)
			}
			if g.BelowBase < 0 {
				t.Errorf("BelowBase 不应为负，实际 %.1f", g.BelowBase)
			}
		})
	}
}

// TestClassifySeaGeometry_MatchesEvaluateHour 锁死「几何判定」与「逐小时评级」的一致性。
//
// 这是 2026-09 那个漏检 bug 的根因防线：
// 只要 ClassifySeaGeometry 判定有云海，EvaluateHour 就必须给出云海类关系；
// 反之只要 EvaluateHour 给出云海类关系，ClassifySeaGeometry 就必须判定有云海。
// 两边一旦再分叉，这个用例立刻失败。
func TestClassifySeaGeometry_MatchesEvaluateHour(t *testing.T) {
	const siteAlt = 1442.0
	th := config.Default().Thresh

	profiles := [][]CloudLayer{
		nil,
		{{BaseMSL: 900, TopMSL: 1062, MaxCC: 70}},
		{{BaseMSL: 300, TopMSL: 1551, MaxCC: 70}},
		{{BaseMSL: 1380, TopMSL: 1500, MaxCC: 70}},
		{
			{BaseMSL: 900, TopMSL: 1062, MaxCC: 70},
			{BaseMSL: 1500, TopMSL: 1650, MaxCC: 40},
		},
		{
			{BaseMSL: 900, TopMSL: 1062, MaxCC: 70},
			{BaseMSL: 1500, TopMSL: 2200, MaxCC: 90},
		},
	}

	for i, layers := range profiles {
		g := ClassifySeaGeometry(siteAlt, layers, th)
		relation, _ := ClassifySite(siteAlt, layers)

		// ClassifySite 是 ClassifySeaGeometry 的上游：
		// 脚下型必须来自 REL_SEA_BELOW 或 REL_OVERHEAD，
		// 淹没型必须来自 REL_IN_CLOUD。
		switch g.Kind {
		case SEA_BELOW:
			if relation != REL_SEA_BELOW && relation != REL_OVERHEAD {
				t.Errorf("廓线#%d：脚下型却来自关系 %q，期望 REL_SEA_BELOW / REL_OVERHEAD",
					i, relation)
			}
		case SEA_SUBMERGED:
			if relation != REL_IN_CLOUD {
				t.Errorf("廓线#%d：淹没型却来自关系 %q，期望 REL_IN_CLOUD", i, relation)
			}
		}
	}
}

// TestHighestBeneathLayer 锁死 HighestBeneath / HighestBeneathLayer 的返回值域。
//
// 不变量：found=true 时 TopMSL 必严格小于机位；否则返回 (0.0,false) / (nil,false)。
// 2026-09 的 Submerged 死代码正是源于误以为 top 可以大于机位。
func TestHighestBeneathLayer(t *testing.T) {
	const siteAlt = 1442.0

	cases := []struct {
		name    string
		topMSL  float64
		wantOK  bool
		wantTop float64
	}{
		{"远低于机位", 1000, true, 1000},
		{"低于机位1m", 1441, true, 1441},
		{"恰好等于机位", 1442, false, 0},
		{"高于机位100m", 1542, false, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			layers := []CloudLayer{{BaseMSL: c.topMSL - 100, TopMSL: c.topMSL}}

			got, ok := HighestBeneath(siteAlt, layers)
			if ok != c.wantOK {
				t.Fatalf("HighestBeneath ok = %v，期望 %v", ok, c.wantOK)
			}
			if ok && got != c.wantTop {
				t.Errorf("HighestBeneath = %.1f，期望 %.1f", got, c.wantTop)
			}
			if ok && got >= siteAlt {
				t.Errorf("不变量被破坏：found=true 时 top=%.1f 必须严格小于机位 %.1f", got, siteAlt)
			}

			l, ok2 := HighestBeneathLayer(siteAlt, layers)
			if ok2 != c.wantOK {
				t.Fatalf("HighestBeneathLayer ok = %v，期望 %v", ok2, c.wantOK)
			}
			if ok2 && l.TopMSL != c.wantTop {
				t.Errorf("HighestBeneathLayer.TopMSL = %.1f，期望 %.1f", l.TopMSL, c.wantTop)
			}
			if !ok2 && l != nil {
				t.Errorf("found=false 时应返回 nil 层，实际 %+v", l)
			}
		})
	}

	if _, ok := HighestBeneath(siteAlt, nil); ok {
		t.Error("空层应返回 false")
	}
	if l, ok := HighestBeneathLayer(siteAlt, nil); ok || l != nil {
		t.Error("空层应返回 (nil,false)")
	}
}
