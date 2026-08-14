package dualtrack

import (
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

const (
	crossTrackSiteAlt = 120.0
	crossTrackPSurf   = 980.0
	crossTrackPSea    = 1013.0
)

func crossTrackInput(cloudBaseAGLM float64) HourInput {
	return HourInput{
		TimeUTC:            time.Date(2026, 8, 12, 14, 0, 0, 0, time.UTC),
		CloudBaseAGLM:      model.Num(cloudBaseAGLM),
		CloudCover:         model.Num(80),
		PressureSurfaceHPa: model.Num(crossTrackPSurf),
		PressureSeaHPa:     model.Num(crossTrackPSea),
	}
}

func TestNegativeCloudBaseAGLMIsRejectedNotSilentlyRated(t *testing.T) {
	th := config.Default().Thresh

	for _, cb := range []float64{-0.001, -1, -500, -1441} {
		v := RateHour(crossTrackInput(cb), crossTrackSiteAlt, &th)

		if !v.DeltaH.Valid || v.DeltaH.V <= 0 {
			t.Fatalf("测试设定错了：载体应为正偏离点位（ΔH > 0），实得 ΔH = %v", v.DeltaH)
		}

		if v.NoDataReason == AmbiguousBase {
			t.Errorf(`CloudBaseAGLM = %.3f（负值，违反「相对模式地形高度 ≥ 0」契约）
被静默归进了**歧义桶**：NoDataReason = %q
Note = %q

这个点位 ΔH = +%.1f 是**正偏离**。按 D6.5 的 M7 恒等式，
cloudBaseAGL ≥ 0 时正偏离在数学上**永不可能**落入歧义桶
（利马 ΔH=+157.8 实测歧义率 0.0%%，就是这条结论的实证）。
它现在落进来了，唯一原因就是入口放行了违约的负值。

期望：判 NODATA 且归因 SemanticFailure（语义失效／契约被违反），
与 AmbiguousBase（真实地理歧义）分开计数。

危害：
  1. 歧义率被污染 —— D6.5 靠歧义率给点位定保真度档位，混入的违约样本会把
     好点位打成差点位，而 ΔH 中位数完全正常，看报告发现不了；
  2. 归因指向错误的排查方向 —— 用户看到「B 轨无云顶字段，无法区分」会以为
     这是 B 轨的能力边界（无解），实际是接线错了（可修）。`,
				cb, v.NoDataReason, v.Note, v.DeltaH.V)
		}

		if v.Rating != model.RATING_NODATA {
			t.Errorf(`CloudBaseAGLM = %.3f 被评了级而不是判 NODATA：
Rating = %s，Note = %q`, cb, v.Rating, v.Note)
		}
	}
}

func TestZeroCloudBaseAGLMStillMeansGroundFog(t *testing.T) {
	th := config.Default().Thresh

	v := RateHour(crossTrackInput(0), 1382.6, &th)

	if v.NoDataReason == SemanticFailure {
		t.Errorf(`cloudBase == 0 被判成了语义失效，但它是**合法输入**：
Tomorrow 用 0 表示「模式地面就是云底」，即接地雾，必须走 D1 接地雾分支。
收紧负值校验时误伤了 0 边界。Note = %q`, v.Note)
	}
	if v.Rating == model.RATING_OK {
		t.Errorf("接地雾（cloudBase == 0）不该评成通透：Rating = %s，Note = %q",
			v.Rating, v.Note)
	}
}

func TestM7IdentityHoldsOnlyWhenCloudBaseNonNegative(t *testing.T) {
	const (
		siteAlt = 160.0
		hModel  = 317.8
	)
	deltaH := hModel - siteAlt
	if deltaH <= 0 {
		t.Fatalf("测试设定错了，ΔH 应为正：%v", deltaH)
	}

	for _, cb := range []float64{0, 1, 100, 5000} {
		if above := deltaH + cb; above <= 0 {
			t.Errorf("M7 恒等式被破坏：ΔH = %.1f > 0 且 cloudBase = %.1f ≥ 0，"+
				"above 却 = %.1f ≤ 0", deltaH, cb, above)
		}
	}

	if above := deltaH + (-1441.0); above > 0 {
		t.Fatalf("测试设定错了：喂 A 轨那种负值（−1441）后 above 应 ≤ 0，实得 %.1f", above)
	}
	t.Logf("已确认：cloudBase = −1441（A 轨括苍山实测量级）会让 ΔH = +%.1f 的"+
		"正偏离点位落入歧义桶，M7「正偏离免疫歧义」结论失效——"+
		"这正是必须在入口拦住负值的原因", deltaH)
}
