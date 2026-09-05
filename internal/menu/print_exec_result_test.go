package menu

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/core"
	"github.com/prophetcro/astro-mountain/internal/report"
)

// TestPrintExecResult_SunriseShowsPerModel 锁死「分歧透明化」在交互菜单结果屏真实可见：
// 喂一条带逐模型朝霞判定的日出结果，printExecResult 必须打印出逐模型明细与分歧行，
// 而不是只列文件路径。这是「把这些模型参数做到交互里」的回归护栏。
func TestPrintExecResult_SunriseShowsPerModel(t *testing.T) {
	var out bytes.Buffer
	s := &state{
		ctx: context.Background(),
		u:   newUI(context.Background(), strings.NewReader("\n"), &out),
	}

	res := core.ExecResult{
		Sunrise: []report.SunriseSiteResult{
			{
				Site:              "太子尖",
				DawnGlow:          "无",
				DawnGlowModels: []report.DawnGlowModelVerdict{
					{Model: "icon_seamless", Tier: "中烧", Primary: true},
					{Model: "gfs_seamless", Tier: "无"},
					{Model: "ecmwf_ifs025", Tier: "中烧"},
					{Model: "best_match", Tier: "无"},
				},
				DawnGlowDivergence: "模型分歧：仅 2/4 判中烧",
			},
		},
	}

	s.printExecResult(res)
	got := out.String()

	mustContain(t, got, "朝霞分歧")
	mustContain(t, got, "逐模型")
	mustContain(t, got, "icon_seamless(主)=中烧")
	mustContain(t, got, "gfs_seamless=无")
	mustContain(t, got, "ecmwf_ifs025=中烧")
	mustContain(t, got, "best_match=无")
	mustContain(t, got, "模型分歧：仅 2/4 判中烧")

	// 单模型（无对比）下不应出现「逐模型」行，应回落说明。
	var out2 bytes.Buffer
	s2 := &state{
		ctx: context.Background(),
		u:   newUI(context.Background(), strings.NewReader("\n"), &out2),
	}
	res2 := core.ExecResult{
		Sunrise: []report.SunriseSiteResult{
			{Site: "冷湖镇", DawnGlow: "无", DawnGlowNote: "低云量极低，无朝霞"},
		},
	}
	s2.printExecResult(res2)
	got2 := out2.String()
	if strings.Contains(got2, "逐模型") {
		t.Errorf("单模型结果不应出现「逐模型」行：\n%s", got2)
	}
	if !strings.Contains(got2, "低云量极低，无朝霞") {
		t.Errorf("单模型应回落到说明行：\n%s", got2)
	}
}
