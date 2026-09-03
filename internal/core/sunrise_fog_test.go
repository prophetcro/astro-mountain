package core

import (
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
)

// setFogSurface 把 resp 全部时次的近地要素覆盖为给定雾场景。
// visValid=false 表示能见度缺测（走代理判据）。
func setFogSurface(resp *api.Response, rh, temp, dew, wind, vis float64, visValid bool) {
	n := len(resp.Times)
	for _, name := range []string{
		"relative_humidity_2m", "temperature_2m", "dew_point_2m", "wind_speed_10m", "visibility",
	} {
		if _, ok := resp.Series[name]; !ok {
			resp.Series[name] = make([]model.OptFloat, n)
		}
		for i := 0; i < n; i++ {
			if name == "visibility" {
				if visValid {
					resp.Series[name][i] = model.Num(vis)
				} else {
					resp.Series[name][i] = model.Missing()
				}
				continue
			}
			var v float64
			switch name {
			case "relative_humidity_2m":
				v = rh
			case "temperature_2m":
				v = temp
			case "dew_point_2m":
				v = dew
			case "wind_speed_10m":
				v = wind
			}
			resp.Series[name][i] = model.Num(v)
		}
	}
}

// TestSunriseGroundFog_Wiring 验证 BuildSunriseReport 把日出窗口内最强近地雾档
// 正确填入 res.FogPotential，且能见度缺测时如实写明代理判定。
func TestSunriseGroundFog_Wiring(t *testing.T) {
	cfg := config.Default()
	night := mergeNight
	sunriseDate := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	resp := makeCloudSeaResp(t)
	setFogSurface(resp, 97, 15.5, 14, 2.5, 0, false) // 能见度缺测 + 强代理

	res := BuildSunriseReport(mergeSite, resp, night, sunriseDate, cfg, 28800, 30)
	if res.FogPotential != profile.FOG_STRONG {
		t.Fatalf("FogPotential=%q，期望 %q", res.FogPotential, profile.FOG_STRONG)
	}
	if !strings.Contains(res.FogNote, "能见度缺测，按近地 RH 代理判定") {
		t.Errorf("FogNote 未写明代理判定：%q", res.FogNote)
	}
}

// TestSunriseGroundFog_DoesNotTouchCloudSeaVerdict 红线回归：
// 同一份气压层廓线（云海判定唯一权威口径）只改地面要素，云海时长 / 形态 /
// 可信度 / 朝霞 / 结论必须逐字节不变；雾档要随地面要素变化（否则本测试无意义）。
//
// 这锁死「近地雾」是一条独立的正面信号，绝不污染云海判定——
// 重演历史上分叉云海判定导致 P0 漏检的教训。
func TestSunriseGroundFog_DoesNotTouchCloudSeaVerdict(t *testing.T) {
	cfg := config.Default()
	night := mergeNight
	sunriseDate := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	strong := makeCloudSeaResp(t)
	setFogSurface(strong, 97, 15.5, 14, 2.5, 0, false) // 强雾、能见度缺测

	none := makeCloudSeaResp(t)
	setFogSurface(none, 40, 25, 10, 2.5, 0, false) // 无雾、能见度缺测

	rS := BuildSunriseReport(mergeSite, strong, night, sunriseDate, cfg, 28800, 30)
	rN := BuildSunriseReport(mergeSite, none, night, sunriseDate, cfg, 28800, 30)

	if rS.CloudSeaHours != rN.CloudSeaHours {
		t.Errorf("云海时长被雾改动：强雾=%d 无雾=%d", rS.CloudSeaHours, rN.CloudSeaHours)
	}
	if rS.CloudSeaForm != rN.CloudSeaForm {
		t.Errorf("云海形态被雾改动：强雾=%q 无雾=%q", rS.CloudSeaForm, rN.CloudSeaForm)
	}
	if rS.Confidence != rN.Confidence {
		t.Errorf("云海可信度被雾改动：强雾=%q 无雾=%q", rS.Confidence, rN.Confidence)
	}
	if rS.DawnGlow != rN.DawnGlow {
		t.Errorf("朝霞档位被雾改动：强雾=%q 无雾=%q", rS.DawnGlow, rN.DawnGlow)
	}
	if rS.Rating != rN.Rating {
		t.Errorf("一句话结论被雾改动：强雾=%q 无雾=%q", rS.Rating, rN.Rating)
	}
	// 反证：雾档确实随地面要素变化了，上面的「不变」断言才有意义。
	if rS.FogPotential == rN.FogPotential {
		t.Errorf("雾档未随地面要素变化：两者都是 %q（测试本身失效）", rS.FogPotential)
	}
}
