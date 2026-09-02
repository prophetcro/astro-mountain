package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/report"
)

// fixedNow 与 cli 测试保持一致的冻结时钟，便于断言默认区间回落。
func sunriseFixedNow() time.Time {
	return time.Date(2026, 8, 1, 21, 30, 0, 0, time.UTC)
}

// TestResolveRangeSunrise 锁死日出模式的区间数学：以「日出当天」为锚，
// 抓数区间为前一夜 00:00 → 日出当天 +1 日 00:00，观测夜 ID 取回拨一天。
func TestResolveRangeSunrise(t *testing.T) {
	p := RunParams{Mode: "sunrise", SunriseDate: "2026-08-14"}
	start, end, nights, desc, err := ResolveRange(p, config.Default().Window)
	if err != nil {
		t.Fatalf("ResolveRange(sunrise) 意外失败：%v", err)
	}
	if got := start.Format(DateLayout); got != "2026-08-13" {
		t.Errorf("start 应为前一夜 2026-08-13，实际 %s", got)
	}
	if got := end.Format(DateLayout); got != "2026-08-15" {
		t.Errorf("end 应为日出当天 +1 日 2026-08-15，实际 %s", got)
	}
	if len(nights) != 1 || nights[0] != "2026-08-13" {
		t.Errorf("观测夜应为 [2026-08-13]，实际 %v", nights)
	}
	if !strings.Contains(desc, "日出当天 2026-08-14") {
		t.Errorf("区间描述应点明日出当天，实际：%s", desc)
	}
}

// TestResolveRangeSunriseMissingDate 锁死：日出模式缺 --sunrise-date 必须报错。
func TestResolveRangeSunriseMissingDate(t *testing.T) {
	p := RunParams{Mode: "sunrise"}
	_, _, _, _, err := ResolveRange(p, config.Default().Window)
	if err == nil {
		t.Fatal("日出模式缺 --sunrise-date 应报错，实际通过")
	}
	if !strings.Contains(err.Error(), "--sunrise-date") {
		t.Errorf("错误信息应点名 --sunrise-date，实际：%v", err)
	}
}

// TestAssessSunriseConfidence 锁死诚实五档可信度的边界，绝不输出伪精度百分比。
func TestAssessSunriseConfidence(t *testing.T) {
	cases := []struct {
		name   string
		hours  int
		eps    int
		vgap   float64
		want   string
	}{
		{"无云海→极低", 0, 0, 100, "极低"},
		{"8h+1段+分辨率足→极高", 9, 1, 100, "极高"},
		{"6h+1段+分辨率足→高", 7, 1, 100, "高"},
		{"3h+1段→中", 4, 1, 100, "中"},
		{"短时段+分辨率足→中", 2, 1, 100, "中"},
		{"分辨率不足→低(即便时次多)", 9, 1, 600, "低"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := assessSunriseConfidence(c.hours, c.eps, c.vgap)
			if got != c.want {
				t.Fatalf("assessSunriseConfidence(%d,%d,%.0f) = %q，期望 %q",
					c.hours, c.eps, c.vgap, got, c.want)
			}
		})
	}
}

// TestAssessDawnGlow 锁死朝霞四档（无/小烧/中烧/大烧）的判定边界。
func TestAssessDawnGlow(t *testing.T) {
	cases := []struct {
		name string
		low  float64
		mid  float64
		high float64
		want string
	}{
		{"低云压顶→无", 70, 50, 50, "无"},
		{"中高云充足→大烧", 10, 50, 40, "大烧"},
		{"中高云中等→中烧", 10, 25, 20, "中烧"},
		{"薄高云→小烧", 5, 2, 8, "小烧"},
		{"无中高云→无", 5, 0, 0, "无"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := assessDawnGlow(c.low, c.mid, c.high)
			if got != c.want {
				t.Fatalf("assessDawnGlow(%.0f,%.0f,%.0f) = %q，期望 %q",
					c.low, c.mid, c.high, got, c.want)
			}
		})
	}
}

// TestSunriseVerdict 锁死聚合结果到一句话结论的映射：极高/高→✅，
// 中→⚠️ 需守候，存疑/无数据→谨慎或放弃。
func TestSunriseVerdict(t *testing.T) {
	cases := []struct {
		name      string
		hours     int
		conf      string
		glow      string
		hasData   bool
		contains  string
	}{
		{"极高→✅", 9, "极高", "大烧", true, "✅"},
		{"高→✅", 7, "高", "中烧", true, "✅"},
		{"中→⚠️", 4, "中", "无", true, "⚠️"},
		{"低(存疑)→⚠️谨慎", 9, "低", "无", true, "谨慎"},
		{"无云海但有朝霞→☀️", 0, "极低", "大烧", true, "☀️"},
		{"无数据→❓", 0, "极低", "无", false, "❓"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := report.SunriseSiteResult{
				Site: "X", CloudSeaHours: c.hours, Confidence: c.conf,
				DawnGlow: c.glow, HasData: c.hasData,
			}
			got := sunriseVerdict(r)
			if !strings.Contains(got, c.contains) {
				t.Fatalf("sunriseVerdict(%+v) = %q，期望包含 %q", r, got, c.contains)
			}
		})
	}
}

// TestRunSunriseRefusesNonOpenMeteo 锁死：日出模式硬塞 Tomorrow.io / Meteoblue
// 时立即中止（退出码 2），绝不用空结果冒充，也不回落 A 轨。
// 该路径在创建取数客户端之前即返回，故无需网络或桩客户端。
func TestRunSunriseRefusesNonOpenMeteo(t *testing.T) {
	cfg := config.Default()
	eng := &Engine{Cfg: cfg, Now: sunriseFixedNow}

	for _, src := range []Source{SourceTomorrow, SourceMeteoblue} {
		t.Run(string(src), func(t *testing.T) {
			p := RunParams{
				Mode:        "sunrise",
				SunriseDate: "2026-08-06", // 落在预报窗口内（距冻结今日 5 天）
				Source:      src,
			}
			res := eng.Run(context.Background(), p)
			if res.ExitCode != 2 {
				t.Fatalf("非 Open-Meteo 的日出模式应退出码 2，实际 %d", res.ExitCode)
			}
			if len(res.Errors) == 0 {
				t.Fatal("应给出错误说明，实际无 Errors")
			}
			if !strings.Contains(res.Errors[0], "日出模式仅支持 Open-Meteo") {
				t.Fatalf("错误应点明日出模式仅支持 Open-Meteo，实际：%v", res.Errors)
			}
			if res.ReportPath != "" {
				t.Fatal("被拒的日出模式不应生成任何报告文件")
			}
		})
	}
}
