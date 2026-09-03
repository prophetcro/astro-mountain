package report

import (
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// TestSunriseMetaWindowReflectsCfg 锁死「夜间窗口」元信息随传入配置动态渲染，
// 而非写死默认 06:00。回归点：修复前 runSunrise 把报告写出传的是 e.Cfg（默认窗），
// 而时段检测用的是放宽到 08:00 的配置副本，导致元信息写 22:00~06:00 却出现 08:00 的时段。
// 该函数本身必须如实反映调用方给的窗口——调用方（kernel.runSunrise）再保证传放宽后的副本。
func TestSunriseMetaWindowReflectsCfg(t *testing.T) {
	meta := model.ReportMeta{
		GeneratedAt:    "2026-09-03 09:00:00",
		Models:         "icon_seamless",
		Timezone:       "Asia/Shanghai",
		UTCOffsetHours: 8,
		Nights:         []string{"2026-09-04"},
		Sites:          []model.Site{{Name: "测试点", Lat: 30, Lon: 120, Alt: 1000}},
	}

	t.Run("默认窗→06:00", func(t *testing.T) {
		cfg := config.Default()
		if cfg.Window.NightEndHour != 6 {
			t.Fatalf("默认 NightEndHour 期望 6，得到 %d", cfg.Window.NightEndHour)
		}
		out := BuildSunriseMarkdownReport(nil, meta, cfg)
		if !strings.Contains(out, "22:00 ~ 06:00") {
			t.Errorf("默认配置元信息应含「22:00 ~ 06:00」，实际未找到：\n%s", excerpt(out, "夜间窗口"))
		}
		if strings.Contains(out, "22:00 ~ 08:00") {
			t.Errorf("默认配置元信息不应含「22:00 ~ 08:00」")
		}
	})

	t.Run("放宽窗→08:00", func(t *testing.T) {
		cfg := config.Default()
		cfg.Window.NightEndHour = 8 // 模拟 runSunrise 放宽到含日出的副本
		out := BuildSunriseMarkdownReport(nil, meta, cfg)
		if !strings.Contains(out, "22:00 ~ 08:00") {
			t.Errorf("放宽配置元信息应含「22:00 ~ 08:00」，实际未找到：\n%s", excerpt(out, "夜间窗口"))
		}
	})
}

// excerpt 返回含 key 的那一行，便于测试失败时打印上下文。
func excerpt(s, key string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, key) {
			return line
		}
	}
	return "(无匹配行)"
}

// TestSunriseConfidenceLabelIsPlain 锁死用户可见的可信度标签为「**可信度**：」，
// 不得出现冗余的「诚实五档」前缀——它与同报告表格头 / 终端输出已用的「可信度」对齐。
// 五档分级（极高/高/中/低/极低）作为可信度取值保留，仅去掉表面冗余前缀。
func TestSunriseConfidenceLabelIsPlain(t *testing.T) {
	meta := model.ReportMeta{
		GeneratedAt:    "2026-09-03 09:00:00",
		Models:         "icon_seamless",
		Timezone:       "Asia/Shanghai",
		UTCOffsetHours: 8,
		Nights:         []string{"2026-09-04"},
		Sites:          []model.Site{{Name: "测试点", Lat: 30, Lon: 120, Alt: 1000}},
	}
	cfg := config.Default()

	r := SunriseSiteResult{
		Site:           "测试点",
		SunriseTime:    time.Date(2026, 9, 4, 5, 30, 0, 0, time.UTC),
		ArriveBy:       time.Date(2026, 9, 4, 4, 0, 0, 0, time.UTC),
		CloudSeaHours:  3,
		HasData:        true,
		DawnGlow:       "中烧",
		DawnGlowNote:   "云顶高度适中，朝霞中等强度",
		Confidence:     "中",
		ConfidenceNote: "云海检出 3 时次、1 段",
		Rating:         "✅ 可蹲守",
	}
	out := BuildSunriseMarkdownReport([]SunriseSiteResult{r}, meta, cfg)

	if !strings.Contains(out, "**可信度**：") {
		t.Errorf("日出报告应渲染「**可信度**：」标签，实际未找到：\n%s", excerpt(out, "可信度"))
	}
	if strings.Contains(out, "**诚实五档可信度**：") {
		t.Errorf("日出报告不得再出现冗余的「诚实五档」前缀：\n%s", excerpt(out, "诚实五档"))
	}
	// 五档分级作为取值仍应正常出现（不为空、非伪造百分比）。
	if !strings.Contains(out, "中 — 云海检出 3 时次") {
		t.Errorf("可信度取值（五档分级）应照常渲染，实际：\n%s", excerpt(out, "可信度"))
	}
}
