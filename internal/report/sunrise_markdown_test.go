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

// TestSunriseConfidenceLabelIsPlain 锁死用户可见的可信度标签为「**云海可信度**：」，
// 不得出现冗余的「诚实五档」前缀，也不得再写成笼统的「可信度」——后者会被误以为
// 是评估朝霞的（朝霞有自己独立的四档「强度」）。云海形态（脚下型/淹没型）单独成行展示。
// 五档分级（极高/高/中/低/极低）作为云海可信度取值保留，仅去掉表面冗余前缀。
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

	t.Run("有云海→标注云海可信度与形态", func(t *testing.T) {
		r := SunriseSiteResult{
			Site:           "测试点",
			SunriseTime:    time.Date(2026, 9, 4, 5, 30, 0, 0, time.UTC),
			ArriveBy:       time.Date(2026, 9, 4, 4, 0, 0, 0, time.UTC),
			CloudSeaHours:  3,
			HasData:        true,
			CloudSeaForm:   "脚下型",
			DawnGlow:       "中烧",
			DawnGlowNote:   "云顶高度适中，朝霞中等强度",
			Confidence:     "中",
			ConfidenceNote: "云海检出 3 时次、1 段",
			Rating:         "✅ 可蹲守",
		}
		out := BuildSunriseMarkdownReport([]SunriseSiteResult{r}, meta, cfg)

		if !strings.Contains(out, "**云海可信度**：") {
			t.Errorf("日出报告应渲染「**云海可信度**：」标签，实际未找到：\n%s", excerpt(out, "云海可信度"))
		}
		if strings.Contains(out, "**可信度**：") {
			t.Errorf("日出报告不得再写成笼统的「可信度」（易与朝霞强度混淆）：\n%s", excerpt(out, "可信度"))
		}
		if strings.Contains(out, "**诚实五档可信度**：") {
			t.Errorf("日出报告不得再出现冗余的「诚实五档」前缀：\n%s", excerpt(out, "诚实五档"))
		}
		if !strings.Contains(out, "**云海形态**：脚下型") {
			t.Errorf("有云海时应渲染「**云海形态**：脚下型」，实际：\n%s", excerpt(out, "云海形态"))
		}
		// 五档分级作为取值仍应正常出现（不为空、非伪造百分比）。
		if !strings.Contains(out, "中 — 云海检出 3 时次") {
			t.Errorf("云海可信度取值（五档分级）应照常渲染，实际：\n%s", excerpt(out, "云海可信度"))
		}
	})

	t.Run("无云海→隐去形态行且可信度为极低", func(t *testing.T) {
		r := SunriseSiteResult{
			Site:           "测试点",
			SunriseTime:    time.Date(2026, 9, 4, 5, 30, 0, 0, time.UTC),
			ArriveBy:       time.Date(2026, 9, 4, 4, 0, 0, 0, time.UTC),
			CloudSeaHours:  0,
			HasData:        true,
			CloudSeaForm:   "", // 无云海时段，形态为空，渲染层应跳过该行
			DawnGlow:       "无",
			DawnGlowNote:   "无中高云载体",
			Confidence:     "极低",
			ConfidenceNote: "预报窗口内未检出云海（机位下方无连续云面）",
			Rating:         "🔴 该夜无云海、朝霞亦弱",
		}
		out := BuildSunriseMarkdownReport([]SunriseSiteResult{r}, meta, cfg)

		if strings.Contains(out, "**云海形态**") {
			t.Errorf("无云海时不应渲染云海形态行：\n%s", excerpt(out, "云海形态"))
		}
		if !strings.Contains(out, "**云海可信度**：极低") {
			t.Errorf("无云海时云海可信度应为极低：\n%s", excerpt(out, "云海可信度"))
		}
	})
}

// TestSunriseReportMultiDateGrouping 锁死「日出模式加多日」的渲染分节：
// 多个日出当天的结果必须按「站点」分节（### 站点名，其下 #### 日出当天 DATE 逐日列出），
// 而非按日期分节——后者会把同一站点散落到多个日期标题下、在折叠重复锚点的预览器里只显第一天。
// 元信息「日出当天」行仍显示天数区间，综合结论（第三节）保留按日期分节的逐日汇总表，
// 且每个站点的云海可信度/形态标签仍正常渲染。
func TestSunriseReportMultiDateGrouping(t *testing.T) {
	meta := model.ReportMeta{
		GeneratedAt:    "2026-09-03 09:00:00",
		Models:         "icon_seamless",
		Timezone:       "Asia/Shanghai",
		UTCOffsetHours: 8,
		Nights:         []string{"2026-09-03", "2026-09-04"},
		Sites:          []model.Site{{Name: "测试点", Lat: 30, Lon: 120, Alt: 1000}},
	}
	cfg := config.Default()

	results := []SunriseSiteResult{
		{
			Site: "测试点", SunriseDate: "2026-09-04",
			SunriseTime: time.Date(2026, 9, 4, 5, 30, 0, 0, time.UTC),
			ArriveBy:    time.Date(2026, 9, 4, 4, 0, 0, 0, time.UTC),
			CloudSeaHours: 3, HasData: true, CloudSeaForm: "脚下型",
			DawnGlow: "中烧", DawnGlowNote: "云顶高度适中",
			Confidence: "中", ConfidenceNote: "云海检出 3 时次", Rating: "✅ 可蹲守",
		},
		{
			Site: "测试点", SunriseDate: "2026-09-05",
			SunriseTime: time.Date(2026, 9, 5, 5, 30, 0, 0, time.UTC),
			ArriveBy:    time.Date(2026, 9, 5, 4, 0, 0, 0, time.UTC),
			CloudSeaHours: 0, HasData: true, CloudSeaForm: "",
			DawnGlow: "无", DawnGlowNote: "无中高云载体",
			Confidence: "极低", ConfidenceNote: "未检出云海", Rating: "🔴 该夜无云海",
		},
	}

	out := BuildSunriseMarkdownReport(results, meta, cfg)

	// 按站点分节：同一站点只占一个 H3，其下逐日（H4）列出。
	if !strings.Contains(out, "### 测试点") {
		t.Errorf("报告应含按站点分节的「### 测试点」：\n%s", excerpt(out, "测试点"))
	}
	if !strings.Contains(out, "#### 日出当天 2026-09-04") {
		t.Errorf("按站点分节后，测试点下应含「#### 日出当天 2026-09-04」：\n%s", excerpt(out, "日出当天 2026-09-04"))
	}
	if !strings.Contains(out, "#### 日出当天 2026-09-05") {
		t.Errorf("按站点分节后，测试点下应含「#### 日出当天 2026-09-05」：\n%s", excerpt(out, "日出当天 2026-09-05"))
	}
	// 元信息仍显示天数区间。
	if !strings.Contains(out, "2 天（2026-09-04 ~ 2026-09-05）") {
		t.Errorf("元信息「日出当天」应显示 2 天区间：\n%s", excerpt(out, "日出当天"))
	}
	// 综合结论（第三节）仍按日期分节给出逐日汇总表。
	if !strings.Contains(out, "### 日出当天 2026-09-04") {
		t.Errorf("综合结论仍应按日期分节含「### 日出当天 2026-09-04」：\n%s", excerpt(out, "日出当天 2026-09-04"))
	}
	// 多日时每站点字段级标签仍必须正确渲染。
	if !strings.Contains(out, "**云海可信度**：") {
		t.Errorf("多日报告仍应渲染「**云海可信度**：」标签")
	}
	if !strings.Contains(out, "**云海形态**：脚下型") {
		t.Errorf("有云海日期应渲染「**云海形态**：脚下型」")
	}
	// 文件名也应体现多日区间。
	fn := sunriseReportFilename(results)
	if fn != "astro_report_sunrise-2026-09-04_2026-09-05.md" {
		t.Errorf("多日文件名应为区间形式，实际 %q", fn)
	}
}

