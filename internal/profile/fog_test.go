package profile

import (
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// fogSurf 构造近地雾判定所需的地面要素。visValid=false 表示能见度缺测（走代理判据）。
func fogSurf(rh, temp, dew, wind, vis float64, visValid bool) model.Surface {
	s := model.Surface{
		RelativeHumidity2m: model.Num(rh),
		Temperature2m:      model.Num(temp),
		DewPoint2m:         model.Num(dew),
		WindSpeed10m:       model.Num(wind),
	}
	if visValid {
		s.Visibility = model.Num(vis)
	}
	return s
}

// TestAssessGroundFog_Levels 覆盖四档：能见度权威（强/中）、能见度良好仅弱、
// 能见度缺测回落代理判据（强/中/无）。
func TestAssessGroundFog_Levels(t *testing.T) {
	cases := []struct {
		name string
		surf model.Surface
		want string
	}{
		{"能见度权威·强(<1000m)", fogSurf(96, 11, 10, 2.5, 800, true), FOG_STRONG},
		{"能见度权威·中(<5000m)", fogSurf(96, 11, 10, 2.5, 3000, true), FOG_MODERATE},
		{"能见度良好·近地饱和仅弱", fogSurf(96, 11, 10, 2.5, 20000, true), FOG_WEAK},
		{"能见度缺测·代理强", fogSurf(96, 11.2, 10, 2.5, 0, false), FOG_STRONG},
		// 风速 4m/s 落在最优区间之外、也未到破坏阈值，辐射雾修正不介入，档位保持代理「中」。
		{"能见度缺测·代理中", fogSurf(92, 13, 10, 4, 0, false), FOG_MODERATE},
		{"能见度缺测·代理无", fogSurf(50, 20, 10, 2.5, 0, false), FOG_NONE},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := AssessGroundFog(c.surf, config.Thresholds{})
			if a.Level != c.want {
				t.Fatalf("Level=%q，期望 %q（Note=%q）", a.Level, c.want, a.Note)
			}
		})
	}
}

// TestAssessGroundFog_VisibilityMissingProxyNote 能见度缺测必须如实写明走代理判据。
func TestAssessGroundFog_VisibilityMissingProxyNote(t *testing.T) {
	a := AssessGroundFog(fogSurf(96, 11.2, 10, 2.5, 0, false), config.Thresholds{})
	if !strings.Contains(a.Note, "能见度缺测，按近地 RH 代理判定") {
		t.Fatalf("能见度缺测时 Note 未写明代理判定：%q", a.Note)
	}
}

// TestAssessGroundFog_ThresholdFallback 配置阈值缺失（零值）必须退回内置默认：
// 既不能把「无」误判成「强」，也能正常给出档位。
func TestAssessGroundFog_ThresholdFallback(t *testing.T) {
	a := AssessGroundFog(model.Surface{Visibility: model.Num(800)}, config.Thresholds{})
	if a.Level != FOG_STRONG {
		t.Fatalf("空配置下能见度 800m 应判强，实际 %q", a.Level)
	}
	b := AssessGroundFog(fogSurf(50, 20, 10, 2.5, 0, false), config.Thresholds{})
	if b.Level != FOG_NONE {
		t.Fatalf("空配置下低湿应判无，实际 %q（阈值=0 不能伪造强）", b.Level)
	}
}

// TestAssessGroundFog_RadiationAdjust 辐射雾修正：
//   - 静风微扰 + 晴空 + 中高云适中 → 命中「雾与朝霞同框」窗口，理由点出；
//   - 风速过大（>5m/s）湍流破坏逆温层 → 代理中下调一档为弱。
func TestAssessGroundFog_RadiationAdjust(t *testing.T) {
	s := model.Surface{
		RelativeHumidity2m: model.Num(96),
		Temperature2m:      model.Num(11.2),
		DewPoint2m:         model.Num(10),
		WindSpeed10m:       model.Num(2.5),
		CloudCoverLow:      model.Num(10),
		CloudCoverMid:      model.Num(60),
		CloudCoverHigh:     model.Num(0),
	}
	a := AssessGroundFog(s, config.Thresholds{})
	if a.Level != FOG_STRONG {
		t.Fatalf("辐射雾加成后应为强，实际 %q（Note=%q）", a.Level, a.Note)
	}
	if !strings.Contains(a.Note, "辐射雾最有利区间") {
		t.Errorf("Note 未提辐射雾最有利风速：%q", a.Note)
	}
	if !strings.Contains(a.Note, "雾与朝霞同框") {
		t.Errorf("Note 未点出雾与朝霞同框窗口：%q", a.Note)
	}

	disrupt := model.Surface{
		RelativeHumidity2m: model.Num(92),
		Temperature2m:      model.Num(13),
		DewPoint2m:         model.Num(10),
		WindSpeed10m:       model.Num(8),
	}
	d := AssessGroundFog(disrupt, config.Thresholds{})
	if d.Level != FOG_WEAK {
		t.Fatalf("大风破坏逆温层应下调为弱，实际 %q（Note=%q）", d.Level, d.Note)
	}
	if !strings.Contains(d.Note, "湍流破坏逆温层") {
		t.Errorf("Note 未说明大风破坏逆温层：%q", d.Note)
	}
}

// TestAssessGroundFog_NoSignalNoFabrication 档位为「无」时辐射雾修正完全不介入，
// 绝不凭空造信号（这是防止修正项变成新伪精度的硬约束）。
func TestAssessGroundFog_NoSignalNoFabrication(t *testing.T) {
	a := AssessGroundFog(fogSurf(50, 20, 10, 2.5, 0, false), config.Thresholds{})
	if a.Level != FOG_NONE {
		t.Fatalf("近地未达雾条件应判无，实际 %q", a.Level)
	}
}
