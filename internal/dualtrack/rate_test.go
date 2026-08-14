package dualtrack

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func pressuresForHModel(t *testing.T, targetH float64) (surface, sea float64) {
	t.Helper()
	const seaLevel = 1013.25
	surface = seaLevel * math.Pow(1.0-targetH/44330.0, 5.255)
	got := HModel(surface, seaLevel)
	if math.Abs(got-targetH) > 1e-6 {
		t.Fatalf("脚手架自检失败：想构造 H_model=%.3f，回代得到 %.6f", targetH, got)
	}
	return surface, seaLevel
}

func hourAt(hModelM float64) HourInput {

	const seaLevel = 1013.25
	surface := seaLevel * math.Pow(1.0-hModelM/44330.0, 5.255)
	return HourInput{
		TimeUTC:              time.Date(2026, 8, 13, 14, 0, 0, 0, time.UTC),
		CloudCover:           model.Num(0),
		VisibilityKm:         model.Num(30),
		HumidityPct:          model.Num(40),
		WindSpeedMS:          model.Num(3),
		PrecipProbabilityPct: model.Num(0),
		PressureSurfaceHPa:   model.Num(surface),
		PressureSeaHPa:       model.Num(seaLevel),
	}
}

func hourFor(t *testing.T, hModelM float64, cloudBase model.OptFloat) HourInput {
	t.Helper()
	surface, sea := pressuresForHModel(t, hModelM)
	in := hourAt(hModelM)
	in.PressureSurfaceHPa = model.Num(surface)
	in.PressureSeaHPa = model.Num(sea)
	in.CloudBaseAGLM = cloudBase
	return in
}

func defaultTh() config.Thresholds { return config.Default().Thresh }

func num(v float64) model.OptFloat { return model.Num(v) }

func TestRateHourBranchTable(t *testing.T) {
	th := defaultTh()
	const siteAlt = 1382.6

	type want struct {
		rel    string
		rating string
		reason NoDataReason
		note   string
	}

	cases := []struct {
		name          string
		hModelM       float64
		cloudBaseAGLm model.OptFloat
		mutate        func(*HourInput)
		want          want
	}{
		{
			name:          "D1_1_云底null_云量低_主路径通透",
			hModelM:       356,
			cloudBaseAGLm: model.Missing(),
			want: want{model.REL_CLEAR, model.RATING_OK, NoDataNone,
				"数据源语义=没云"},
		},
		{
			name:          "D1_1_云底null_云量也缺测_不得按晴空处理",
			hModelM:       356,
			cloudBaseAGLm: model.Missing(),
			mutate:        func(in *HourInput) { in.CloudCover = model.Missing() },
			want: want{model.REL_NODATA, model.RATING_NODATA, KeyMissing,
				"不按晴空处理"},
		},
		{
			name:          "D1_1_云底null_却报云量85_语义失效",
			hModelM:       356,
			cloudBaseAGLm: model.Missing(),
			mutate:        func(in *HourInput) { in.CloudCover = model.Num(85) },
			want: want{model.REL_NODATA, model.RATING_NODATA, SemanticFailure,
				"自相矛盾"},
		},
		{
			name:          "D6.2_云底有效但地面气压缺测_不借邻近时次",
			hModelM:       356,
			cloudBaseAGLm: num(800),
			mutate:        func(in *HourInput) { in.PressureSurfaceHPa = model.Missing() },
			want: want{model.REL_NODATA, model.RATING_NODATA, KeyMissing,
				"不借邻近时次回填"},
		},
		{
			name:          "D6.2_云底有效但海平面气压缺测",
			hModelM:       356,
			cloudBaseAGLm: num(800),
			mutate:        func(in *HourInput) { in.PressureSeaHPa = model.Missing() },
			want:          want{model.REL_NODATA, model.RATING_NODATA, KeyMissing, ""},
		},
		{

			name:          "D1_2a_模式地面有雾_地形≈机位_接地雾BAD",
			hModelM:       siteAlt,
			cloudBaseAGLm: num(0),
			want:          want{model.REL_OVERHEAD, model.RATING_BAD, NoDataNone, "接地雾"},
		},
		{

			name:          "D1_2b_模式地面有雾_地形远低于机位_歧义桶",
			hModelM:       356,
			cloudBaseAGLm: num(0),
			want: want{model.REL_BASE_BELOW_UNKNOWN, model.RATING_NODATA, AmbiguousBase,
				"无法判定机位在雾上"},
		},
		{

			name:          "D1_2c_模式地面有雾_地形高于机位_雾在头顶WARN",
			hModelM:       1800,
			cloudBaseAGLm: num(0),
			want: want{model.REL_OVERHEAD, model.RATING_WARN, NoDataNone,
				"位于机位上方"},
		},
		{

			name:          "D6.4_3a_云底低于机位_歧义桶",
			hModelM:       356,
			cloudBaseAGLm: num(800),
			want: want{model.REL_BASE_BELOW_UNKNOWN, model.RATING_NODATA, AmbiguousBase,
				"无法区分"},
		},
		{

			name:          "D6.4_3b_云底刚过机位_低云底BAD",
			hModelM:       siteAlt,
			cloudBaseAGLm: num(0.001),
			want:          want{model.REL_OVERHEAD, model.RATING_BAD, NoDataNone, "低云底"},
		},
		{

			name:          "D6.4_3b_云底逼近LCL告警阈值_低云底BAD",
			hModelM:       siteAlt,
			cloudBaseAGLm: num(99),
			want:          want{model.REL_OVERHEAD, model.RATING_BAD, NoDataNone, "低云底"},
		},
		{

			name:          "D6.4_3c_云底在高处_正常通透",
			hModelM:       siteAlt,
			cloudBaseAGLm: num(2000),
			want: want{model.REL_OVERHEAD, model.RATING_OK, NoDataNone,
				"云底在头顶 2000m"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := hourFor(t, c.hModelM, c.cloudBaseAGLm)
			if c.mutate != nil {
				c.mutate(&in)
			}

			v := RateHour(in, siteAlt, &th)

			if v.Rel != c.want.rel {
				t.Errorf("Rel = %s，期望 %s", v.Rel, c.want.rel)
			}
			if v.Rating != c.want.rating {
				t.Errorf("Rating = %s，期望 %s（Note: %s）", v.Rating, c.want.rating, v.Note)
			}
			if v.NoDataReason != c.want.reason {
				t.Errorf("NoDataReason = %q，期望 %q", v.NoDataReason, c.want.reason)
			}
			if c.want.note != "" && !strings.Contains(v.Note, c.want.note) {
				t.Errorf("Note 未包含 %q，实际为 %q", c.want.note, v.Note)
			}

			if (v.Rating == model.RATING_NODATA) != (v.NoDataReason != NoDataNone) {
				t.Errorf("Rating 与 NoDataReason 不自洽：%s / %q", v.Rating, v.NoDataReason)
			}

			if v.Rel == model.REL_SEA_BELOW || v.Rel == model.REL_IN_CLOUD {
				t.Errorf("B 轨不得产出 %s", v.Rel)
			}

			if !v.SeaBelowUnknown {
				t.Errorf("SeaBelowUnknown 必须恒为 true：%+v", v)
			}
		})
	}
}

func TestDeltaHNeverEntersRatingChain(t *testing.T) {
	th := defaultTh()
	const siteAlt = 1382.6
	const wantAbove = 2000.0

	seen := map[TerrainFidelity]bool{}
	var firstRel, firstRating string

	for i, h := range []float64{1382.6, 1300, 1500, 1000, 1800, 356, 2400} {
		cb := wantAbove + siteAlt - h
		if cb <= 0 {
			t.Fatalf("用例构造错误：cloudBase=%.1f 不该 ≤0", cb)
		}
		v := RateHour(hourFor(t, h, num(cb)), siteAlt, &th)

		if math.Abs(v.CloudBaseAboveSite.V-wantAbove) > 1e-6 {
			t.Fatalf("H_model=%.1f：above = %.6f，构造有误（应恒为 %.1f）",
				h, v.CloudBaseAboveSite.V, wantAbove)
		}
		if i == 0 {
			firstRel, firstRating = v.Rel, v.Rating
		}
		if v.Rel != firstRel || v.Rating != firstRating {
			t.Errorf("H_model=%.1f（ΔH=%.1f）：判定变成 %s/%s，"+
				"但 above 没变（%.1f）——说明 ΔH 泄进了评级链",
				h, v.DeltaH.V, v.Rel, v.Rating, wantAbove)
		}
		if !v.DeltaH.Valid || math.Abs(v.DeltaH.V-(h-siteAlt)) > 1e-6 {
			t.Errorf("H_model=%.1f：DeltaH = %+v，期望 %.6f", h, v.DeltaH, h-siteAlt)
		}
		seen[v.TerrainFidelity] = true
	}

	for _, f := range []TerrainFidelity{TerrainFaithful, TerrainCoarse, TerrainFlattened} {
		if !seen[f] {
			t.Errorf("用例没覆盖到保真度档位 %s，测试强度不足", f)
		}
	}
}

func TestVisibilityKmConvertedToMeters(t *testing.T) {
	th := defaultTh()
	const siteAlt = 1382.6
	const hModel = 1382.6

	cases := []struct {
		name       string
		visKm      float64
		wantRating string
		wantNote   string
	}{
		{"0.5km=500m_低于雾阈值1000m_压成BAD", 0.5, model.RATING_BAD, "能见度 500m"},
		{"3km=3000m_低于霾阈值5000m_压成WARN", 3, model.RATING_WARN, "轻雾/霾"},
		{"5km=5000m_恰好等于霾阈值_不降级", 5, model.RATING_OK, ""},
		{"30km_通透", 30, model.RATING_OK, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := hourFor(t, hModel, num(2000))
			in.VisibilityKm = num(c.visKm)

			v := RateHour(in, siteAlt, &th)

			if v.Rating != c.wantRating {
				t.Errorf("visibility=%.1fkm：Rating = %s，期望 %s（Note: %s）；"+
					"若 %.1fkm 被当成 %.1f 米比较，说明少乘了 1000",
					c.visKm, v.Rating, c.wantRating, v.Note, c.visKm, c.visKm)
			}
			if c.wantNote != "" && !strings.Contains(v.Note, c.wantNote) {
				t.Errorf("Note 未包含 %q，实际为 %q", c.wantNote, v.Note)
			}
		})
	}
}

func TestClearOverlayOnlyDegrades(t *testing.T) {
	th := defaultTh()
	const siteAlt = 1382.6

	in := hourFor(t, siteAlt, num(0))
	in.VisibilityKm = num(30)
	in.HumidityPct = num(20)

	v := RateHour(in, siteAlt, &th)
	if v.Rating != model.RATING_BAD {
		t.Fatalf("接地雾遇上完美能见度被洗成了 %s，Worse 语义被破坏（Note: %s）",
			v.Rating, v.Note)
	}
}

func TestPrecipProbNoteOnlyNeverChangesRating(t *testing.T) {
	th := defaultTh()
	const siteAlt = 1382.6

	base := hourFor(t, siteAlt, num(2000))
	dry := RateHour(base, siteAlt, &th)

	wet := base
	wet.PrecipProbabilityPct = num(90)
	got := RateHour(wet, siteAlt, &th)

	if got.Rating != dry.Rating {
		t.Errorf("降水概率 90%% 改变了评级：%s → %s，应只追加 Note",
			dry.Rating, got.Rating)
	}
	if !strings.Contains(got.Note, "降水概率 90%") {
		t.Errorf("降水概率达标却没进 Note，实际为 %q", got.Note)
	}
	if !strings.Contains(got.Note, "仅提示，不改评级") {
		t.Errorf("Note 必须写明不改评级，实际为 %q", got.Note)
	}
}

func TestRateSeriesPreservesOrderAndLength(t *testing.T) {
	th := defaultTh()
	const siteAlt = 1382.6

	mk := func(hourUTC int, cb model.OptFloat) HourInput {
		in := hourFor(t, siteAlt, cb)
		in.TimeUTC = time.Date(2026, 8, 13, hourUTC, 0, 0, 0, time.UTC)
		return in
	}

	got, err := RateSeries([]HourInput{
		mk(12, num(3000)),
		mk(13, num(50)),
		mk(14, model.Missing()),
	}, siteAlt, DatumAGL, &th)
	if err != nil {
		t.Fatalf("RateSeries 报错：%v", err)
	}
	if len(got) != 3 {
		t.Fatalf("返回 %d 条，期望 3 条（不得截断）", len(got))
	}

	wantRating := []string{model.RATING_OK, model.RATING_BAD, model.RATING_OK}
	wantRel := []string{model.REL_OVERHEAD, model.REL_OVERHEAD, model.REL_CLEAR}
	for i := range got {
		if got[i].Rating != wantRating[i] || got[i].Rel != wantRel[i] {
			t.Errorf("第 %d 条 = %s/%s，期望 %s/%s（顺序可能被打乱）",
				i, got[i].Rel, got[i].Rating, wantRel[i], wantRating[i])
		}
		if got[i].TimeUTC.Hour() != 12+i {
			t.Errorf("第 %d 条 TimeUTC = %v，期望 %d 时", i, got[i].TimeUTC, 12+i)
		}
	}
}

func TestRateSeriesEmptyIsEmptyNotError(t *testing.T) {
	th := defaultTh()
	got, err := RateSeries(nil, 1382.6, DatumAGL, &th)
	if err != nil {
		t.Fatalf("空序列不该报错，得到 %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("空序列应返回 0 条，得到 %d 条", len(got))
	}
}

func TestThresholdsNilFallsBackToDefaults(t *testing.T) {
	const siteAlt = 1382.6
	in := hourFor(t, siteAlt, num(50))

	th := defaultTh()
	withTh := RateHour(in, siteAlt, &th)
	withNil := RateHour(in, siteAlt, nil)

	if withTh.Rating != withNil.Rating || withTh.Rel != withNil.Rel {
		t.Fatalf("th=nil 未回落到出厂阈值：%s/%s vs %s/%s",
			withTh.Rel, withTh.Rating, withNil.Rel, withNil.Rating)
	}
}

func TestTerrainFidelityTiers(t *testing.T) {
	cases := []struct {
		deltaH float64
		want   TerrainFidelity
	}{
		{0, TerrainFaithful},
		{150, TerrainFaithful},
		{-150, TerrainFaithful},
		{150.1, TerrainFaithful},
		{-427, TerrainCoarse},
		{500, TerrainFaithful},
		{500.1, TerrainFaithful},
		{-500, TerrainFlattened},
		{-500.1, TerrainFlattened},
		{-1026.6, TerrainFlattened},
		{math.NaN(), TerrainUnknown},
		{math.Inf(1), TerrainUnknown},
	}
	for _, c := range cases {
		if got := ClassifyTerrainFidelity(c.deltaH); got != c.want {
			t.Errorf("ClassifyTerrainFidelity(%v) = %s，期望 %s", c.deltaH, got, c.want)
		}
	}
	if got := ClassifyTerrainFidelityOpt(model.Missing()); got != TerrainUnknown {
		t.Errorf("缺测 ΔH 应为 TerrainUnknown，得到 %s", got)
	}
	if got := ClassifyTerrainFidelityOpt(num(-427)); got != TerrainCoarse {
		t.Errorf("ΔH=−427 应为 TerrainCoarse，得到 %s", got)
	}
}

func TestTerrainFidelitySigned(t *testing.T) {

	if got := ClassifyTerrainFidelity(158); got != TerrainFaithful {
		t.Errorf("ΔH=+158（利马，正偏离）应为 TerrainFaithful，得到 %s", got)
	}

	if got := ClassifyTerrainFidelity(-158); got != TerrainCoarse {
		t.Errorf("ΔH=−158（同幅值负偏离）应为 TerrainCoarse，得到 %s", got)
	}

	if ClassifyTerrainFidelity(158) == ClassifyTerrainFidelity(-158) {
		t.Error("±158 落到了同一档——分档方案退化为 |ΔH|，D6.5 有符号语义被破坏")
	}

	if got := ClassifyTerrainFidelity(-150); got != TerrainFaithful {
		t.Errorf("ΔH=−150 应为 TerrainFaithful（下界含），得到 %s", got)
	}
	if got := ClassifyTerrainFidelity(-500); got != TerrainFlattened {
		t.Errorf("ΔH=−500 应为 TerrainFlattened（D6.5 边界翻转点），得到 %s", got)
	}

	if got := ClassifyTerrainFidelity(500); got != TerrainFaithful {
		t.Errorf("ΔH=+500（正偏离）应为 TerrainFaithful，得到 %s", got)
	}
}

func TestBoundariesAreClosed(t *testing.T) {
	th := defaultTh()
	const p = 1013.25

	if got := HModel(p, p); got != 0 {
		t.Fatalf("前提不成立：HModel(%[1]v, %[1]v) = %v，不是精确 0，"+
			"本测试的边界断言失去意义", p, got)
	}

	exact := func(cloudBaseM, siteAlt float64) HourVerdict {
		in := HourInput{
			TimeUTC:              time.Date(2026, 8, 13, 14, 0, 0, 0, time.UTC),
			CloudBaseAGLM:        num(cloudBaseM),
			CloudCover:           num(0),
			VisibilityKm:         num(30),
			HumidityPct:          num(40),
			WindSpeedMS:          num(3),
			PrecipProbabilityPct: num(0),
			PressureSurfaceHPa:   num(p),
			PressureSeaHPa:       num(p),
		}
		return RateHour(in, siteAlt, &th)
	}

	t.Run("歧义边界_above恰好为0_取闭区间进歧义桶", func(t *testing.T) {

		v := exact(100, 100)
		if v.CloudBaseAboveSite.V != 0 {
			t.Fatalf("构造有误：above = %v，期望精确 0", v.CloudBaseAboveSite.V)
		}
		if v.Rel != model.REL_BASE_BELOW_UNKNOWN || v.Rating != model.RATING_NODATA {
			t.Errorf("above 恰好为 0（云底与机位齐平）应进歧义桶，得到 %s/%s（%s）",
				v.Rel, v.Rating, v.Note)
		}
		if v.NoDataReason != AmbiguousBase {
			t.Errorf("NoDataReason = %q，期望 AmbiguousBase", v.NoDataReason)
		}
	})

	t.Run("歧义边界_above最小正值_离开歧义桶但仍不通透", func(t *testing.T) {

		v := exact(100.001, 100)
		if v.Rating == model.RATING_OK {
			t.Fatalf("云底只高出机位 1mm 就评通透了：%s", v.Note)
		}
		if v.Rel != model.REL_OVERHEAD || v.Rating != model.RATING_BAD {
			t.Errorf("期望 REL_OVERHEAD/RATING_BAD，得到 %s/%s（%s）",
				v.Rel, v.Rating, v.Note)
		}
	})

	t.Run("LCL告警阈值_above恰好等于100_闭区间取BAD", func(t *testing.T) {

		v := exact(th.LCLAlertAGLM, 0)
		if v.CloudBaseAboveSite.V != th.LCLAlertAGLM {
			t.Fatalf("构造有误：above = %v，期望精确 %v",
				v.CloudBaseAboveSite.V, th.LCLAlertAGLM)
		}
		if v.Rating != model.RATING_BAD {
			t.Errorf("above 恰好等于 LCL 告警阈值 %.0fm 应取闭区间判 BAD，得到 %s（%s）",
				th.LCLAlertAGLM, v.Rating, v.Note)
		}
	})

	t.Run("LCL告警阈值_above刚过100_转正常评级", func(t *testing.T) {
		v := exact(th.LCLAlertAGLM+0.001, 0)
		if v.Rel != model.REL_OVERHEAD || v.Rating != model.RATING_OK {
			t.Errorf("above 超过 LCL 告警阈值应转正常评级，得到 %s/%s（%s）",
				v.Rel, v.Rating, v.Note)
		}
	})

	t.Run("接地雾边界_ΔH恰好为150_算贴合机位判BAD", func(t *testing.T) {

		v := exact(0, -TerrainFaithfulMaxM)
		if v.DeltaH.V != TerrainFaithfulMaxM {
			t.Fatalf("构造有误：ΔH = %v，期望精确 %v", v.DeltaH.V, TerrainFaithfulMaxM)
		}
		if v.Rating != model.RATING_BAD || !strings.Contains(v.Note, "接地雾") {
			t.Errorf("|ΔH| 恰好 150m 应判接地雾 BAD，得到 %s（%s）", v.Rating, v.Note)
		}
		if v.TerrainFidelity != TerrainFaithful {
			t.Errorf("|ΔH|=150 应为 TerrainFaithful，得到 %s", v.TerrainFidelity)
		}
	})

	t.Run("接地雾边界_ΔH刚过-150_转歧义桶", func(t *testing.T) {

		v := exact(0, TerrainFaithfulMaxM+0.001)
		if v.Rel != model.REL_BASE_BELOW_UNKNOWN || v.Rating != model.RATING_NODATA {
			t.Errorf("ΔH 刚过 −150m 应进歧义桶，得到 %s/%s（%s）",
				v.Rel, v.Rating, v.Note)
		}
	})
}
