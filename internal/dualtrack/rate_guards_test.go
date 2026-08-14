package dualtrack

import (
	"errors"
	"math"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/model"
)

func TestCloudBaseTakenAsMetersNoKmMultiply(t *testing.T) {
	th := defaultTh()

	cases := []struct {
		name string

		cloudBaseAGLm float64
		hModelM       float64
		siteAlt       float64
		wantAbove     float64
	}{
		{
			name:          "原始km_0.8km已换算成800m_括苍山",
			cloudBaseAGLm: 800,
			hModelM:       356,
			siteAlt:       1382.6,
			wantAbove:     356 + 800 - 1382.6,
		},
		{
			name:          "原始ft_5000ft已换算成1524m",
			cloudBaseAGLm: 1524,
			hModelM:       356,
			siteAlt:       1382.6,
			wantAbove:     356 + 1524 - 1382.6,
		},
		{
			name:          "原始m_1200m原样",
			cloudBaseAGLm: 1200,
			hModelM:       930,
			siteAlt:       1382.6,
			wantAbove:     930 + 1200 - 1382.6,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := hourFor(t, c.hModelM, num(c.cloudBaseAGLm))
			v := RateHour(in, c.siteAlt, &th)

			if !v.CloudBaseAboveSite.Valid {
				t.Fatalf("cloudBaseAboveSite 不该缺测：%+v", v)
			}
			if math.Abs(v.CloudBaseAboveSite.V-c.wantAbove) > 1e-6 {
				t.Errorf("cloudBaseAboveSite = %.6f，期望 %.6f（差 %.1f）；"+
					"若相差约 1000 倍说明有人对已换算的米值又乘了一次系数",
					v.CloudBaseAboveSite.V, c.wantAbove,
					v.CloudBaseAboveSite.V-c.wantAbove)
			}

			if !v.CloudBaseAGLM.Valid || v.CloudBaseAGLM.V != c.cloudBaseAGLm {
				t.Errorf("CloudBaseAGLM 回显 = %+v，期望原样 %.6f",
					v.CloudBaseAGLM, c.cloudBaseAGLm)
			}

			bogus := c.hModelM + c.cloudBaseAGLm*1000 - c.siteAlt
			if math.Abs(v.CloudBaseAboveSite.V-bogus) < 1e-6 {
				t.Fatalf("cloudBaseAboveSite 命中了 ×1000 的错误值 %.1f", bogus)
			}
		})
	}
}

func TestDatumNotAGLRejected(t *testing.T) {
	t.Run("RequireAGLDatum_逐取值", func(t *testing.T) {

		for _, ok := range []string{DatumAGL, "AGL", " agl ", "Agl"} {
			if err := RequireAGLDatum(ok); err != nil {
				t.Errorf("datum=%q 不该报错，得到 %v", ok, err)
			}
		}

		for _, bad := range []string{"msl", "MSL", "", "  ", "wgs84", "ellipsoid"} {
			err := RequireAGLDatum(bad)
			if err == nil {
				t.Errorf("datum=%q 必须报错，却放行了", bad)
				continue
			}
			if !errors.Is(err, ErrDatumNotAGL) {
				t.Errorf("datum=%q 的错误应可被 errors.Is(ErrDatumNotAGL) 命中，得到 %v",
					bad, err)
			}
		}
	})

	t.Run("RateSeries_非AGL必须报错且不产出任何结果", func(t *testing.T) {
		th := defaultTh()
		in := []HourInput{hourFor(t, 356, num(800))}

		got, err := RateSeries(in, 1382.6, "msl", &th)
		if err == nil {
			t.Fatalf("msl 基准下 RateSeries 必须报错，却返回了 %d 条结果", len(got))
		}
		if !errors.Is(err, ErrDatumNotAGL) {
			t.Fatalf("错误应可被 errors.Is(ErrDatumNotAGL) 命中，得到 %v", err)
		}

		if got != nil {
			t.Fatalf("报错时不得返回半成品结果，得到 %d 条", len(got))
		}
	})

	t.Run("重复扣减的错误值量级正常_证明只能靠入口断言", func(t *testing.T) {

		const siteAlt, hModel, cloudBaseM = 1382.6, 356.0, 800.0
		correct := hModel + cloudBaseM - siteAlt

		mslPseudoAGL := cloudBaseM - siteAlt
		doubled := hModel + mslPseudoAGL - siteAlt

		if math.Signbit(correct) != math.Signbit(doubled) {
			t.Fatalf("前提已变：正确值 %.1f 与重复扣减值 %.1f 符号不同了，"+
				"错误变得可被下游发现，请重新评估 RequireAGLDatum 的注释",
				correct, doubled)
		}
		if math.Abs(doubled) > 100000 {
			t.Fatalf("前提已变：重复扣减值 %.1f 量级已经离谱到能被发现", doubled)
		}
	})
}

func TestAmbiguousNeverOK(t *testing.T) {
	th := defaultTh()

	hModels := []float64{-200, 0, 120, 356, 500, 930, 1300, 1382.6, 1500, 2400}
	cloudBases := []float64{0, 0.5, 1, 10, 50, 200, 800, 1200, 3000}
	siteAlts := []float64{0, 120, 800, 1382.6, 1800, 2400, 3300}

	checked := 0
	for _, h := range hModels {
		for _, cb := range cloudBases {
			for _, alt := range siteAlts {
				above := h + cb - alt
				if above > 0 {
					continue
				}
				checked++

				v := RateHour(hourFor(t, h, num(cb)), alt, &th)

				if v.Rating == model.RATING_OK {
					t.Fatalf("云底低于机位却评成通透：H_model=%.1f cloudBase=%.1f "+
						"siteAlt=%.1f above=%.1f → %s/%s（%s）",
						h, cb, alt, above, v.Rel, v.Rating, v.Note)
				}

				if v.Rel == model.REL_SEA_BELOW || v.Rel == model.REL_IN_CLOUD {
					t.Fatalf("B 轨没有云顶字段，不得产出 %s：H_model=%.1f "+
						"cloudBase=%.1f siteAlt=%.1f", v.Rel, h, cb, alt)
				}
				if !v.SeaBelowUnknown {
					t.Errorf("SeaBelowUnknown 必须恒为 true：%+v", v)
				}

				gotAbove := v.CloudBaseAboveSite.V
				if math.Abs(gotAbove-above) > 1e-6 {
					t.Fatalf("H_model 反解误差超出容忍：H_model=%.1f cloudBase=%.1f "+
						"siteAlt=%.1f，算得 above=%.9f，理论 %.9f",
						h, cb, alt, gotAbove, above)
				}
				if gotAbove > 0 {
					continue
				}

				if cb <= 0 && gotAbove >= -TerrainFaithfulMaxM {
					continue
				}
				if v.Rel != model.REL_BASE_BELOW_UNKNOWN {
					t.Errorf("H_model=%.1f cloudBase=%.1f siteAlt=%.1f above=%.1f："+
						"Rel = %s，期望 REL_BASE_BELOW_UNKNOWN", h, cb, alt, above, v.Rel)
				}
				if v.Rating != model.RATING_NODATA {
					t.Errorf("H_model=%.1f cloudBase=%.1f siteAlt=%.1f above=%.1f："+
						"Rating = %s，期望 RATING_NODATA", h, cb, alt, above, v.Rating)
				}
				if v.NoDataReason != AmbiguousBase {
					t.Errorf("H_model=%.1f cloudBase=%.1f siteAlt=%.1f："+
						"NoDataReason = %q，期望 AmbiguousBase（不能混进 KeyMissing，"+
						"否则用户会以为是接口坏了而不是能力边界）",
						h, cb, alt, v.NoDataReason)
				}
				if !v.IsAmbiguous() {
					t.Errorf("IsAmbiguous() 应为 true：%+v", v)
				}
			}
		}
	}

	if checked < 50 {
		t.Fatalf("只穷举到 %d 个歧义组合，样本网格可能被改窄了，守卫已失效", checked)
	}
	t.Logf("穷举了 %d 个 cloudBaseAboveSite ≤ 0 的组合，全部未评 OK", checked)
}
