package dualtrack

import (
	"math"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/model"
)

func TestHModelKuocangShanMeasured(t *testing.T) {

	got := HModel(962.16, 1003.76)
	if got < 350 || got > 365 {
		t.Fatalf("HModel = %.1f，期望落在 [350,365]。括号写错会得到 24191", got)
	}
}

func TestHModelColdLakeCrossCheck(t *testing.T) {

	const realAltM = 2790.0
	got := HModel(723.47, 996.06)

	if got < 2610 || got > 2625 {
		t.Fatalf("冷湖 H_model = %.1f，期望落在 [2610,2625]（实测 2616.9）。"+
			"若得到 2678，说明 pSea 被换成了括苍山的 1003.76", got)
	}

	coldLakeGap := realAltM - got
	kuocangGap := 1382.6 - HModel(962.16, 1003.76)
	if kuocangGap <= coldLakeGap {
		t.Errorf("括苍山地形差 %.0f m 应远大于冷湖 %.0f m——"+
			"孤立山头才是被网格抹平的那个", kuocangGap, coldLakeGap)
	}

	if kuocangGap < coldLakeGap*4 {
		t.Errorf("括苍山 %.0f m 与冷湖 %.0f m 未拉开数量级差距，"+
			"本用例的核心论据失效", kuocangGap, coldLakeGap)
	}
}

func TestHModelRejectsCrossSiteSplice(t *testing.T) {
	const coldLakeSurface, kuocangSea = 723.47, 1003.76
	spliced := HModel(coldLakeSurface, kuocangSea)
	genuine := HModel(coldLakeSurface, 996.06)

	if math.Abs(spliced-genuine) < 30 {
		t.Fatalf("拼接对解出 %.1f、真实对解出 %.1f，差距不足 30 m，"+
			"本用例失去区分能力", spliced, genuine)
	}

	if spliced < 0 || spliced > 9000 {
		t.Errorf("拼接对解出 %.1f，落在了明显异常的区间——"+
			"若真如此反而好办了，本用例的前提（错误不可见）已不成立", spliced)
	}
}

func TestHModelWrongParenthesesIsOrderOfMagnitudeOff(t *testing.T) {
	const surface, sea = 962.16, 1003.76
	right := HModel(surface, sea)
	wrong := barometricScaleM * math.Pow(1.0-surface/sea, barometricExp)

	if math.Abs(right-wrong) < 1000 {
		t.Fatalf("正确写法 %.1f 与错误写法 %.1f 相差不足 1000 m，"+
			"本用例失去了区分能力，请检查是否改动了公式", right, wrong)
	}
	if wrong < 20000 {
		t.Errorf("错误写法应算出约 24191 m，实际 %.1f——"+
			"若此处变了，说明常数被改过，正确性断言也要重新校准", wrong)
	}
}

func TestHModelAtSeaLevelIsZero(t *testing.T) {
	got := HModel(1013.25, 1013.25)
	if math.Abs(got) > 1e-9 {
		t.Errorf("两压相等时 HModel = %g，期望 0", got)
	}
}

func TestHModelIsMonotonic(t *testing.T) {
	const sea = 1013.25
	prev := math.Inf(-1)

	for _, surface := range []float64{1013.25, 1000, 950, 900, 850, 700, 500} {
		got := HModel(surface, sea)
		if got <= prev {
			t.Fatalf("surface=%.0f 时 HModel=%.1f，未严格大于上一档 %.1f", surface, got, prev)
		}
		prev = got
	}
}

func TestHModelStandardAtmosphereLandmarks(t *testing.T) {
	const sea = 1013.25
	cases := []struct {
		surface  float64
		lo, hi   float64
		landmark string
	}{
		{850, 1400, 1600, "850 hPa ≈ 1.5 km"},
		{700, 2900, 3200, "700 hPa ≈ 3 km"},
		{500, 5400, 5800, "500 hPa ≈ 5.5 km"},
	}
	for _, c := range cases {
		got := HModel(c.surface, sea)
		if got < c.lo || got > c.hi {
			t.Errorf("%s：HModel(%.0f, %.2f) = %.1f，期望落在 [%.0f,%.0f]",
				c.landmark, c.surface, sea, got, c.lo, c.hi)
		}
	}
}

func TestHModelBelowSeaLevelIsNegativeNotNaN(t *testing.T) {
	got := HModel(1030, 1013.25)
	if math.IsNaN(got) {
		t.Fatal("本站气压高于海压时不该返回 NaN，负高度是合法结果")
	}
	if got >= 0 {
		t.Errorf("HModel = %.1f，期望为负", got)
	}
}

func TestHModelRejectsInvalidDomain(t *testing.T) {
	cases := []struct {
		name         string
		surface, sea float64
	}{
		{"海压为 0", 962.16, 0},
		{"海压为负", 962.16, -1013.25},
		{"本站气压为负", -1, 1013.25},
		{"本站气压 NaN", math.NaN(), 1013.25},
		{"海压 NaN", 962.16, math.NaN()},
		{"本站气压 +Inf", math.Inf(1), 1013.25},
		{"海压 -Inf", 962.16, math.Inf(-1)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HModel(c.surface, c.sea); !math.IsNaN(got) {
				t.Errorf("HModel(%g, %g) = %g，非法定义域应返回 NaN", c.surface, c.sea, got)
			}
		})
	}
}

func TestHModelZeroSurfaceIsFinite(t *testing.T) {
	got := HModel(0, 1013.25)
	if math.IsNaN(got) || math.IsInf(got, 0) {
		t.Fatalf("surface=0 应给出有限值，实际 %g", got)
	}
	if math.Abs(got-barometricScaleM) > 1e-9 {
		t.Errorf("surface=0 时 HModel = %.3f，期望等于压高系数 %.1f", got, barometricScaleM)
	}
}

func TestHModelOptMissingPropagates(t *testing.T) {
	valid := model.Num(962.16)
	miss := model.Missing()

	cases := []struct {
		name         string
		surface, sea model.OptFloat
	}{
		{"本站气压缺测", miss, model.Num(1003.76)},
		{"海压缺测", valid, miss},
		{"两者都缺测", miss, miss},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := HModelOpt(c.surface, c.sea)
			if got.Valid {
				t.Errorf("应缺测，实际 %+v", got)
			}
			if v := got.Or(-1); v != -1 {
				t.Errorf("缺测值不该被当成 0 用，Or(-1) = %g", v)
			}
		})
	}
}

func TestHModelOptHappyPath(t *testing.T) {
	got := HModelOpt(model.Num(962.16), model.Num(1003.76))
	if !got.Valid {
		t.Fatal("两个气压都有效时不该缺测")
	}
	if got.V < 350 || got.V > 365 {
		t.Errorf("HModelOpt = %.1f，期望落在 [350,365]", got.V)
	}
}

func TestHModelOptConvertsNaNToMissing(t *testing.T) {
	got := HModelOpt(model.Num(962.16), model.Num(0))
	if got.Valid {
		t.Errorf("非法定义域应转成缺测，实际 %+v", got)
	}
}

func TestHModelPerStep(t *testing.T) {

	const sea = 1003.76
	fine := surfaceForHeight(930, sea)
	coarse := surfaceForHeight(356, sea)

	const fineSteps, totalSteps = 16, 120
	surface := make([]model.OptFloat, totalSteps)
	seaSeries := make([]model.OptFloat, totalSteps)
	for i := range surface {
		if i < fineSteps {
			surface[i] = model.Num(fine)
		} else {
			surface[i] = model.Num(coarse)
		}
		seaSeries[i] = model.Num(sea)
	}

	got := SeriesHModel(surface, seaSeries)
	if len(got) != totalSteps {
		t.Fatalf("输出长度 %d，期望 %d", len(got), totalSteps)
	}
	for i, h := range got {
		if !h.Valid {
			t.Fatalf("step %d 不该缺测", i)
		}
		want := 356.0
		if i < fineSteps {
			want = 930.0
		}
		if math.Abs(h.V-want) > 1 {
			t.Errorf("step %d 的 H_model = %.1f，期望约 %.0f——"+
				"若这里趋同了，说明有人做了跨时次聚合", i, h.V, want)
		}
	}

	if math.Abs(got[0].V-got[totalSteps-1].V) < 500 {
		t.Errorf("首尾 H_model 分别为 %.1f / %.1f，差距不足 500 m，"+
			"模式切换特征已被抹平", got[0].V, got[totalSteps-1].V)
	}
}

func TestSeriesHModelMissingIsPerStep(t *testing.T) {
	surface := []model.OptFloat{
		model.Num(962.16), model.Missing(), model.Num(962.16), model.Num(962.16),
	}
	sea := []model.OptFloat{
		model.Num(1003.76), model.Num(1003.76), model.Missing(), model.Num(1003.76),
	}

	got := SeriesHModel(surface, sea)
	if len(got) != 4 {
		t.Fatalf("输出长度 %d，期望 4", len(got))
	}
	for _, i := range []int{0, 3} {
		if !got[i].Valid {
			t.Errorf("step %d 两个气压都有效，不该缺测", i)
		}
	}
	for _, i := range []int{1, 2} {
		if got[i].Valid {
			t.Errorf("step %d 有气压缺测，必须 NODATA，实际 %+v——"+
				"不许借邻近时次兜底", i, got[i])
		}
	}
}

func TestSeriesHModelLengthMismatchReturnsNil(t *testing.T) {
	surface := []model.OptFloat{model.Num(962.16), model.Num(962.16)}
	sea := []model.OptFloat{model.Num(1003.76)}
	if got := SeriesHModel(surface, sea); got != nil {
		t.Errorf("长度不一致应返回 nil，实际 %+v", got)
	}
	if got := SeriesHModel(sea, surface); got != nil {
		t.Errorf("长度不一致（反向）应返回 nil，实际 %+v", got)
	}
}

func TestSeriesHModelEmptyIsEmptyNotNil(t *testing.T) {
	got := SeriesHModel([]model.OptFloat{}, []model.OptFloat{})
	if got == nil {
		t.Fatal("两条空序列是合法输入，应返回空切片而不是 nil（nil 表示长度不一致）")
	}
	if len(got) != 0 {
		t.Errorf("空输入应得空输出，实际长度 %d", len(got))
	}
}

func surfaceForHeight(heightM, seaHPa float64) float64 {
	return seaHPa * math.Pow(1.0-heightM/barometricScaleM, 5.255)
}

func TestSurfaceForHeightRoundTrip(t *testing.T) {
	const sea = 1003.76
	for _, h := range []float64{0, 100, 356, 930, 1382.6, 3000} {
		p := surfaceForHeight(h, sea)
		back := HModel(p, sea)
		if math.Abs(back-h) > 1e-6 {
			t.Errorf("H=%.1f 反推气压 %.4f 再正算得 %.6f，往返不自洽", h, p, back)
		}
	}
}
