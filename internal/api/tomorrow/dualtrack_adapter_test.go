package tomorrow

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func dtPressuresFor(t *testing.T, targetH float64) (surface, sea float64) {
	t.Helper()
	const seaLevel = 1013.25
	surface = seaLevel * math.Pow(1.0-targetH/44330.0, 5.255)
	if got := dualtrack.HModel(surface, seaLevel); math.Abs(got-targetH) > 1e-6 {
		t.Fatalf("脚手架自检失败：想构造 H_model=%.3f，回代得到 %.6f", targetH, got)
	}
	return surface, seaLevel
}

func dtSampleAt(t *testing.T, hModelM float64, hourUTC int) Sample {
	t.Helper()
	surface, sea := dtPressuresFor(t, hModelM)
	return Sample{
		TimeUTC:              time.Date(2026, 8, 13, hourUTC, 0, 0, 0, time.UTC),
		CloudCover:           model.Num(0),
		VisibilityKm:         model.Num(30),
		HumidityPct:          model.Num(40),
		WindSpeedMS:          model.Num(3),
		PrecipProbabilityPct: model.Num(0),
		PressureSurfaceHPa:   model.Num(surface),
		PressureSeaHPa:       model.Num(sea),
	}
}

func dtTh() config.Thresholds { return config.Default().Thresh }

func TestCloudBaseUnitsReachDualTrackAsMetersOnly(t *testing.T) {
	th := dtTh()
	const siteAlt = 1382.6
	const hModel = 356.0

	cases := []struct {
		name      string
		raw       float64
		unit      Unit
		wantM     float64
		wantAbove float64
	}{
		{"原始km_0.8km→800m", 0.8, UnitKilometer, 800, 356 + 800 - 1382.6},
		{"原始ft_5000ft→1524m", 5000, UnitFeet, 1524, 356 + 1524 - 1382.6},
		{"原始m_1200m原样", 1200, UnitMeter, 1200, 356 + 1200 - 1382.6},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotM, err := ToMeters(c.raw, c.unit)
			if err != nil {
				t.Fatalf("ToMeters(%v, %q) 报错：%v", c.raw, c.unit, err)
			}
			if math.Abs(gotM-c.wantM) > 1e-9 {
				t.Fatalf("ToMeters(%v, %q) = %v，期望 %v（换算系数被改了？）",
					c.raw, c.unit, gotM, c.wantM)
			}

			sr := &SiteResult{
				Site:         model.Site{Name: "括苍山", Alt: siteAlt},
				Samples:      []Sample{dtSampleAt(t, hModel, 14)},
				ResolvedUnit: c.unit,
				CloudBaseAGL: []model.OptFloat{model.Num(gotM)},
			}

			got, err := RateSite(sr, DatumAGL, &th)
			if err != nil {
				t.Fatalf("RateSite 报错：%v", err)
			}
			if len(got) != 1 {
				t.Fatalf("返回 %d 条，期望 1 条", len(got))
			}

			above := got[0].CloudBaseAboveSite
			if !above.Valid {
				t.Fatalf("cloudBaseAboveSite 不该缺测：%+v", got[0])
			}
			if math.Abs(above.V-c.wantAbove) > 1e-6 {
				t.Errorf("cloudBaseAboveSite = %.6f，期望 %.6f；"+
					"若相差约 1000 倍说明对已换算的米值又乘了一次系数",
					above.V, c.wantAbove)
			}

			if bogus := hModel + gotM*1000 - siteAlt; math.Abs(above.V-bogus) < 1e-6 {
				t.Fatalf("cloudBaseAboveSite 命中了 ×1000 的错误值 %.1f", bogus)
			}

			if got[0].CloudBaseAGLM.V != gotM {
				t.Errorf("CloudBaseAGLM 回显 = %v，期望原样 %v",
					got[0].CloudBaseAGLM.V, gotM)
			}
		})
	}
}

func TestDualTrackInputsIgnoresMSLFields(t *testing.T) {
	th := dtTh()
	const siteAlt = 1382.6
	s := dtSampleAt(t, 356, 14)

	build := func(msl model.OptFloat) *SiteResult {
		return &SiteResult{
			Site:            model.Site{Name: "括苍山", Alt: siteAlt},
			Samples:         []Sample{s},
			ResolvedUnit:    UnitMeter,
			CloudBaseAGL:    []model.OptFloat{model.Num(800)},
			CloudCeilingAGL: []model.OptFloat{model.Missing()},
			CloudBaseMSL:    []model.OptFloat{msl},
			CloudCeilingMSL: []model.OptFloat{msl},
		}
	}

	base, err := RateSite(build(model.Num(800+siteAlt)), DatumAGL, &th)
	if err != nil {
		t.Fatalf("RateSite（正常 MSL）报错：%v", err)
	}
	poisoned, err := RateSite(build(model.Num(999999)), DatumAGL, &th)
	if err != nil {
		t.Fatalf("RateSite（投毒 MSL）报错：%v", err)
	}

	if base[0].Rating != poisoned[0].Rating ||
		base[0].Rel != poisoned[0].Rel ||
		base[0].CloudBaseAboveSite != poisoned[0].CloudBaseAboveSite {
		t.Fatalf("把 MSL 字段投毒成 999999 改变了结论，说明评级链读了这两个字段：\n"+
			"  正常 = %s/%s/%+v\n  投毒 = %s/%s/%+v",
			base[0].Rel, base[0].Rating, base[0].CloudBaseAboveSite,
			poisoned[0].Rel, poisoned[0].Rating, poisoned[0].CloudBaseAboveSite)
	}

	if base[0].Rel != model.REL_BASE_BELOW_UNKNOWN ||
		base[0].Rating != model.RATING_NODATA {
		t.Errorf("括苍山 above=−226.6m 应进歧义桶，得到 %s/%s（%s）",
			base[0].Rel, base[0].Rating, base[0].Note)
	}
}

func TestRateSiteRejectsNonAGLDatum(t *testing.T) {
	th := dtTh()
	sr := &SiteResult{
		Site:         model.Site{Name: "括苍山", Alt: 1382.6},
		Samples:      []Sample{dtSampleAt(t, 356, 14)},
		ResolvedUnit: UnitMeter,
		CloudBaseAGL: []model.OptFloat{model.Num(800)},
	}

	got, err := RateSite(sr, DatumMSL, &th)
	if err == nil {
		t.Fatalf("DatumMSL 下 RateSite 必须报错，却返回了 %d 条结果", len(got))
	}
	if !errors.Is(err, dualtrack.ErrDatumNotAGL) {
		t.Fatalf("错误应可被 errors.Is(dualtrack.ErrDatumNotAGL) 命中，得到 %v", err)
	}

	if got != nil {
		t.Fatalf("报错时不得返回半成品结果，得到 %d 条", len(got))
	}

	norm, perr := ParseDatum("")
	if perr != nil {
		t.Fatalf("ParseDatum(\"\") 不该报错：%v", perr)
	}
	if norm != DatumAGL {
		t.Fatalf("ParseDatum(\"\") = %q，期望归一成 %q", norm, DatumAGL)
	}
	if _, err := RateSite(sr, norm, &th); err != nil {
		t.Errorf("ParseDatum(\"\") 归一成 agl 后应放行，得到 %v", err)
	}
}

func TestRateSitePairsByIndex(t *testing.T) {
	th := dtTh()
	const siteAlt = 1382.6

	sr := &SiteResult{
		Site: model.Site{Name: "括苍山", Alt: siteAlt},
		Samples: []Sample{
			dtSampleAt(t, siteAlt, 12),
			dtSampleAt(t, siteAlt, 13),
			dtSampleAt(t, siteAlt, 14),
		},
		ResolvedUnit: UnitMeter,
		CloudBaseAGL: []model.OptFloat{
			model.Num(3000),
			model.Num(50),
			model.Missing(),
		},
	}

	got, err := RateSite(sr, DatumAGL, &th)
	if err != nil {
		t.Fatalf("RateSite 报错：%v", err)
	}
	if len(got) != 3 {
		t.Fatalf("返回 %d 条，期望 3 条", len(got))
	}

	wantRating := []string{model.RATING_OK, model.RATING_BAD, model.RATING_OK}
	wantRel := []string{model.REL_OVERHEAD, model.REL_OVERHEAD, model.REL_CLEAR}
	for i := range got {
		if got[i].Rating != wantRating[i] || got[i].Rel != wantRel[i] {
			t.Errorf("第 %d 条 = %s/%s，期望 %s/%s（下标配对可能错位）",
				i, got[i].Rel, got[i].Rating, wantRel[i], wantRating[i])
		}
		if got[i].TimeUTC.Hour() != 12+i {
			t.Errorf("第 %d 条 TimeUTC = %v，期望 %d 时", i, got[i].TimeUTC, 12+i)
		}
	}
}

func TestRateSiteRejectsMalformedInput(t *testing.T) {
	th := dtTh()
	s := dtSampleAt(t, 356, 14)

	sr := &SiteResult{
		Site:         model.Site{Name: "括苍山", Alt: 1382.6},
		Samples:      []Sample{s, s, s},
		CloudBaseAGL: []model.OptFloat{model.Num(800)},
	}
	if _, err := RateSite(sr, DatumAGL, &th); !errors.Is(err, dualtrack.ErrSeriesMalformed) {
		t.Fatalf("长度不自洽必须报 dualtrack.ErrSeriesMalformed，得到 %v", err)
	}
	if _, err := ToDualTrackInputs(nil); err == nil {
		t.Fatal("nil SiteResult 必须报错")
	}
	if _, err := RateSite(nil, DatumAGL, &th); err == nil {
		t.Fatal("nil SiteResult 走 RateSite 也必须报错")
	}
}

func TestToDualTrackInputsCopiesFieldsVerbatim(t *testing.T) {
	surface, sea := dtPressuresFor(t, 356)
	s := Sample{
		TimeUTC:              time.Date(2026, 8, 13, 14, 0, 0, 0, time.UTC),
		CloudCover:           model.Num(11),
		VisibilityKm:         model.Num(22),
		HumidityPct:          model.Num(33),
		WindSpeedMS:          model.Num(44),
		PrecipProbabilityPct: model.Num(55),
		PressureSurfaceHPa:   model.Num(surface),
		PressureSeaHPa:       model.Num(sea),

		CloudBaseRaw:    model.Num(777),
		CloudCeilingRaw: model.Num(888),
		WindGustMS:      model.Num(99),
		TemperatureC:    model.Num(-7),
		DewPointC:       model.Num(-9),
	}
	sr := &SiteResult{
		Site:         model.Site{Name: "括苍山", Alt: 1382.6},
		Samples:      []Sample{s},
		ResolvedUnit: UnitMeter,
		CloudBaseAGL: []model.OptFloat{model.Num(800)},
	}

	in, err := ToDualTrackInputs(sr)
	if err != nil {
		t.Fatalf("ToDualTrackInputs 报错：%v", err)
	}
	if len(in) != 1 {
		t.Fatalf("返回 %d 条，期望 1 条", len(in))
	}
	got := in[0]

	checks := []struct {
		field string
		got   model.OptFloat
		want  float64
	}{

		{"CloudBaseAGLM", got.CloudBaseAGLM, 800},
		{"CloudCover", got.CloudCover, 11},
		{"VisibilityKm", got.VisibilityKm, 22},
		{"HumidityPct", got.HumidityPct, 33},
		{"WindSpeedMS", got.WindSpeedMS, 44},
		{"PrecipProbabilityPct", got.PrecipProbabilityPct, 55},
		{"PressureSurfaceHPa", got.PressureSurfaceHPa, surface},
		{"PressureSeaHPa", got.PressureSeaHPa, sea},
	}
	for _, c := range checks {
		if !c.got.Valid {
			t.Errorf("%s 不该缺测", c.field)
			continue
		}
		if math.Abs(c.got.V-c.want) > 1e-9 {
			t.Errorf("%s = %v，期望 %v（字段可能串位了）", c.field, c.got.V, c.want)
		}
	}
	if !got.TimeUTC.Equal(s.TimeUTC) {
		t.Errorf("TimeUTC = %v，期望 %v", got.TimeUTC, s.TimeUTC)
	}

	if got.CloudBaseAGLM.V == 777 {
		t.Error("CloudBaseAGLM 取到了 Sample.CloudBaseRaw（未换算原始值），" +
			"必须取 SiteResult.CloudBaseAGL[i]")
	}
}

func TestToDualTrackInputsEmptyIsEmptyNotError(t *testing.T) {
	sr := &SiteResult{
		Site:         model.Site{Name: "括苍山", Alt: 1382.6},
		Samples:      []Sample{},
		CloudBaseAGL: []model.OptFloat{},
	}
	got, err := ToDualTrackInputs(sr)
	if err != nil {
		t.Fatalf("空序列不该报错，得到 %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("空序列应返回 0 条，得到 %d 条", len(got))
	}
}
