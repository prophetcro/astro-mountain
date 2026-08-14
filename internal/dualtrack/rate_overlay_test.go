package dualtrack

import (
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/model"
)

func TestVisibilityTakesPriorityOverHumidityProxy(t *testing.T) {
	th := defaultTh()
	const siteAlt = 1382.6

	in := hourFor(t, siteAlt, num(2000))
	in.VisibilityKm = num(30)
	in.HumidityPct = num(99)

	v := RateHour(in, siteAlt, &th)

	if v.Rating != model.RATING_OK {
		t.Errorf(`Rating = %s，期望 %s。
能见度 30km（实测量）已判定通透，湿度 99%%（代理量）不该再降级——
D1 规定 clearOverlay 是"visibility 优先 / humidity 代理判雾"，
两者互斥。若 rate.go 的 switch 被拆成两个独立 if，同一团雾会被计两次。
Note: %s`, v.Rating, model.RATING_OK, v.Note)
	}

	if strings.Contains(v.Note, "代理判据") {
		t.Errorf(`Note 出现了"代理判据"字样，说明能见度有效时湿度分支仍被执行：
%s`, v.Note)
	}
}

func TestHumidityProxyEngagesOnlyWhenVisibilityMissing(t *testing.T) {
	th := defaultTh()
	const siteAlt = 1382.6

	in := hourFor(t, siteAlt, num(2000))
	in.VisibilityKm = model.Missing()
	in.HumidityPct = num(99)

	v := RateHour(in, siteAlt, &th)

	if v.Rating != model.RATING_BAD {
		t.Errorf(`Rating = %s，期望 %s。
能见度缺测时湿度必须作为代理判据接管；否则"没测到能见度"会被当成"没有雾"，
正是缺测安全红线要防的那类降级。Note: %s`, v.Rating, model.RATING_BAD, v.Note)
	}
	if !strings.Contains(v.Note, "代理判据") {
		t.Errorf(`Note 未出现"代理判据"字样，无法向读报告的人说明这一级是怎么来的：
%s`, v.Note)
	}
}

func TestNegativeCloudBaseFallsToConservativeSide(t *testing.T) {
	th := defaultTh()
	const siteAlt = 1382.6

	for _, cb := range []float64{-0.001, -1, -500, -5000} {

		in := hourFor(t, siteAlt, num(cb))
		v := RateHour(in, siteAlt, &th)

		if v.Rating == model.RATING_OK {
			t.Errorf(`cloudBase = %.3fm（负值）判成了 %s。
离地高度为负是脏数据，绝不能评成"头顶通透"——它必须落进接地雾/歧义桶
一侧。若 rate.go:250 的 `+"`<= 0`"+` 被收紧成 `+"`== 0`"+`，负值就会掉进
"云底在模式地面之上"的分支。Rel=%s Note=%s`,
				cb, v.Rating, v.Rel, v.Note)
		}
	}
}
