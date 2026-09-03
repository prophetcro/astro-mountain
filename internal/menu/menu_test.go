package menu

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
)

func frozenNow() time.Time {
	return time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
}

func stubFontProbe(string) (string, error) { return "/fake/font.ttc", nil }

func stubRender(_, outDir, _, _ string) ([]string, error) {
	return []string{
		filepath.Join(outDir, "astro_report_peak-2026-08-13_sites.png"),
		filepath.Join(outDir, "astro_report_peak-2026-08-13_astro.png"),
	}, nil
}

func runMenu(t *testing.T, input string, tweak func(*Options)) (int, string) {
	t.Helper()

	cfg := config.Default()
	cfg.Output.OutDir = t.TempDir()
	cfg.Output.DouyinDir = filepath.Join(cfg.Output.OutDir, "douyin")

	var out bytes.Buffer
	opts := Options{
		Cfg:          cfg,
		ConfigPath:   filepath.Join(t.TempDir(), "config.json"),
		SitesPath:    "",
		Stdin:        strings.NewReader(input),
		Stdout:       &out,
		Version:      "v-test",
		FontProbe:    stubFontProbe,
		RenderReport: stubRender,
		Now:          frozenNow,
	}
	if tweak != nil {
		tweak(&opts)
	}
	code := Run(context.Background(), opts)
	return code, out.String()
}

func mustContain(t *testing.T, out string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("输出中缺少 %q\n---- 实际输出 ----\n%s", w, out)
		}
	}
}

func mustNotContain(t *testing.T, out string, notWant ...string) {
	t.Helper()
	for _, w := range notWant {
		if strings.Contains(out, w) {
			t.Errorf("输出中不该出现 %q\n---- 实际输出 ----\n%s", w, out)
		}
	}
}

func writeSites(t *testing.T, sites []config.Site) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sites.json")
	if err := config.SaveSites(sites, path); err != nil {
		t.Fatalf("准备 sites.json 失败：%v", err)
	}
	return path
}

func site(name string, lat, lon, alt float64) config.Site {
	on := true
	return config.Site{Name: name, Lat: lat, Lon: lon, Alt: alt, Enabled: &on}
}

func TestMainMenuRendersSixEntries(t *testing.T) {
	code, out := runMenu(t, "0\n", nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out,
		"[1]", "生成评估报告",
		"[2]", "仅生成抖音图片",
		"[3]", "点位配置管理",
		"[4]", "运行参数设置",
		"[5]", "查看帮助",
		"[0]", "退出",
	)

	mustContain(t, out, "山地星野 · 低云海拔评估工具", "v-test",
		"点位配置", "运行配置", "输出目录", "中文字体")
}

func TestSelectZeroExitsWithCodeZero(t *testing.T) {
	code, out := runMenu(t, "0\n", nil)
	if code != 0 {
		t.Fatalf("输入 0 的退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "再见")
}

func TestQuitKeywordRequiresConfirmation(t *testing.T) {

	code, out := runMenu(t, "q\nn\n0\n", nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "确定退出？")
	mustContain(t, out, "再见")
}

func TestQuitKeywordConfirmed(t *testing.T) {
	code, _ := runMenu(t, "q\ny\n", nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
}

func TestInvalidChoiceRepromptsWithoutCrashing(t *testing.T) {
	code, out := runMenu(t, "99\nabc\n0\n", nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0（非法输入不应改变退出码）", code)
	}
	mustContain(t, out, `✗ 无效输入`, `"99" 不是可选项`, `"abc" 不是可选项`)

	if n := strings.Count(out, "请选择 [1/2/3/4/5/0]"); n < 3 {
		t.Errorf("主菜单提示出现 %d 次，期望 ≥3（每次非法输入都应重新提示）", n)
	}
}

func TestThreeInvalidInputsFallBackToParent(t *testing.T) {

	code, out := runMenu(t, "x\ny\nz\n", nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "输入多次无效，已返回上级")
}

func TestEOFExitsSafely(t *testing.T) {

	code, out := runMenu(t, "", nil)
	if code != 0 {
		t.Fatalf("EOF 退出码 = %d，期望 0", code)
	}
	mustNotContain(t, out, "panic")
}

func TestEOFMidFlowExitsSafely(t *testing.T) {

	code, out := runMenu(t, "1\n", nil)
	if code != 0 {
		t.Fatalf("流程中 EOF 退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "步骤 1/6：运行模式")
}

func TestCanceledContextExitsGracefully(t *testing.T) {
	cfg := config.Default()
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	code := Run(ctx, Options{
		Cfg:       cfg,
		Stdin:     strings.NewReader("0\n"),
		Stdout:    &out,
		FontProbe: stubFontProbe,
		Now:       frozenNow,
	})
	if code != 0 {
		t.Fatalf("ctx 取消时退出码 = %d，期望 0", code)
	}
	mustContain(t, out.String(), "已收到中断信号")
}

func TestHelpFlowShowsRatingsAndSafetyLine(t *testing.T) {
	_, out := runMenu(t, "5\n\n0\n", nil)
	mustContain(t, out,
		"✅通透", "⚠️风险", "🔴不宜", "❓无数据",
		"安全红线：❓无数据 ≠ 晴朗",
		"--peak", "--menu",
	)
}

func TestSiteManagerListsSites(t *testing.T) {
	path := writeSites(t, []config.Site{
		site("牵牛岗", 30.0260, 119.0070, 1489.9),
		site("括苍山", 28.8101, 120.9221, 1382.6),
	})
	_, out := runMenu(t, "3\nb\n0\n", func(o *Options) { o.SitesPath = path })
	mustContain(t, out,
		"[3] 点位配置管理",
		"牵牛岗", "括苍山",
		"30.0260", "119.0070", "1489.9",
		"[a] 新增点位", "[d] 删除点位", "[s] 保存写回文件",
	)
}

func TestAddSiteRejectsOutOfRangeLatLonAlt(t *testing.T) {
	path := writeSites(t, []config.Site{site("牵牛岗", 30.026, 119.007, 1489.9)})
	input := strings.Join([]string{
		"3",
		"a",
		"大明山",
		"91.5",
		"-90.1",
		"30.0",
		"181",
		"119.0",
		"-600",
		"1489.9",
		"测试备注",
		"y",
		"n",
		"b",
		"0",
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out,
		"须在 -90 ~ 90 之间",
		"须在 -180 ~ 180 之间",
		"须在 -500 ~ 9000 之间",
		"已取消",
	)
}

func TestAddSiteRejectsDuplicateName(t *testing.T) {
	path := writeSites(t, []config.Site{site("牵牛岗", 30.026, 119.007, 1489.9)})
	input := strings.Join([]string{
		"3", "a",
		"牵牛岗",
		"大明山",
		"30.0", "119.0", "1400",
		"",
		"y",
		"n",
		"b", "0",
	}, "\n") + "\n"

	_, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	mustContain(t, out, "点位名「牵牛岗」已存在")
}

func TestAddSiteRejectsEmptyAndOverlongName(t *testing.T) {
	path := writeSites(t, []config.Site{site("牵牛岗", 30.026, 119.007, 1489.9)})
	long := strings.Repeat("很长的名字", 5)
	input := strings.Join([]string{
		"3", "a",
		"",
		long,
		"大明山",
		"30.0", "119.0", "1400", "", "y", "n",
		"b", "0",
	}, "\n") + "\n"

	_, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	mustContain(t, out, "不能为空", "超过 16 字符")
}

func TestDeleteSiteRequiresDoubleConfirmation(t *testing.T) {
	path := writeSites(t, []config.Site{
		site("牵牛岗", 30.026, 119.007, 1489.9),
		site("括苍山", 28.8101, 120.9221, 1382.6),
	})

	input := "3\nd\n1\nn\nb\n0\n"
	_, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	mustContain(t, out, "此操作不可撤销", "已取消删除")

	res, err := config.LoadSites(path)
	if err != nil {
		t.Fatalf("重新加载失败：%v", err)
	}
	if len(res.Sites) != 2 {
		t.Errorf("取消删除后点位数 = %d，期望 2", len(res.Sites))
	}
}

func TestSaveSitesWritesFileAndBackup(t *testing.T) {
	path := writeSites(t, []config.Site{
		site("牵牛岗", 30.026, 119.007, 1489.9),
		site("括苍山", 28.8101, 120.9221, 1382.6),
	})

	input := "3\nd\n2\ny\ns\ny\nb\n0\n"
	code, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "已保存 1 个点位")

	res, err := config.LoadSites(path)
	if err != nil {
		t.Fatalf("重新加载失败：%v", err)
	}
	if len(res.Sites) != 1 || res.Sites[0].Name != "牵牛岗" {
		t.Errorf("保存后点位 = %+v，期望只剩牵牛岗", res.Sites)
	}

	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Errorf("未生成备份文件 %s.bak：%v", path, err)
	}

	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Errorf("残留临时文件 %s.tmp，原子替换未清理干净", path)
	}
}

func TestToggleSiteEnabled(t *testing.T) {
	path := writeSites(t, []config.Site{
		site("牵牛岗", 30.026, 119.007, 1489.9),
		site("括苍山", 28.8101, 120.9221, 1382.6),
	})
	input := "3\nt\n2\ns\ny\nb\n0\n"
	_, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	mustContain(t, out, "点位「括苍山」已停用")

	res, _ := config.LoadSites(path)
	if len(res.Enabled()) != 1 {
		t.Errorf("停用后启用点位数 = %d，期望 1", len(res.Enabled()))
	}
}

func TestUnsavedSitesWarnOnQuit(t *testing.T) {
	path := writeSites(t, []config.Site{
		site("牵牛岗", 30.026, 119.007, 1489.9),
		site("括苍山", 28.8101, 120.9221, 1382.6),
	})

	input := "3\nt\n1\nb\nn\n0\ny\n"
	code, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "有未保存的点位修改", "点位配置 有未保存的修改，退出后将丢失")
}

func TestValidateConfigCommandReportsProblems(t *testing.T) {
	path := writeSites(t, []config.Site{site("牵牛岗", 30.026, 119.007, 1489.9)})
	_, out := runMenu(t, "3\nc\nb\n0\n", func(o *Options) { o.SitesPath = path })
	mustContain(t, out, "配置校验通过（1 个点位，1 启用）")
}

func TestReportFlowRejectsBadDates(t *testing.T) {
	input := strings.Join([]string{
		"1", // 主菜单 [1]
		"1", // 步骤 1/6 模式 → 流星雨
		"1", // 步骤 2/6 日期 → 极大日 + 天数
		"2026-13-01",
		"2026-02-30",
		"abcd-ef-gh",

		"0",
	}, "\n") + "\n"

	code, out := runMenu(t, input, nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0（非法日期不应导致崩溃或非零退出）", code)
	}
	mustContain(t, out,
		"月份 13 不存在",
		"2026 年 2 月没有 30 日",
		"日期只能由数字与短横线组成",
		"输入多次无效，已返回上级",
	)
}

func TestReportFlowStepsAndBackNavigation(t *testing.T) {
	path := writeSites(t, []config.Site{
		site("牵牛岗", 30.026, 119.007, 1489.9),
		site("括苍山", 28.8101, 120.9221, 1382.6),
	})
	input := strings.Join([]string{
		"1", // 主菜单 → [1] 生成评估报告
		"1", // 步骤 1/6 模式 → 流星雨
		"1", // 步骤 2/6 日期 → 极大日 + 天数
		"2026-08-13",
		"2",
		"1", // 步骤 3/6 点位 → 全部
		"", // 步骤 4/6 导出 → 确认
		"", // 步骤 5/6 高级 → 跳过
		"Y", // 步骤 6/6 确认 → 执行
		"", // 执行后 pause
		"0", // 主菜单退出
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out,
		"步骤 1/6：运行模式",
		"步骤 2/6：日期范围（流星雨）",
		"已确定观测夜：2026-08-11 ~ 2026-08-13，共 3 夜",
		"步骤 3/6：点位选择",
		"步骤 4/6：导出内容",
		"步骤 5/6：高级选项",
		"── 确认 ",
		"等价命令 astro-mountain --peak 2026-08-13 --days 2",
		"执行内核未注入",
	)
}

func TestReportFlowCustomRangeRejectsReversedDates(t *testing.T) {
	input := strings.Join([]string{
		"1", // 主菜单 [1]
		"1", // 步骤 1/6 模式 → 流星雨
		"2", // 步骤 2/6 日期 → 自定义区间
		"2026-08-14",
		"2026-08-10",
		"2026-08-10",
		"2026-08-12",
		"b", // 步骤 3/6 点位 → 返回日期
		"b", // 步骤 2/6 日期 → 返回模式
		"b", // 步骤 1/6 模式 → 返回主菜单
		"0",
	}, "\n") + "\n"

	code, out := runMenu(t, input, nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "结束日期不能早于起始日期",
		"已确定观测夜：2026-08-10 ~ 2026-08-12，共 3 夜")
}

func TestReportFlowWithoutEngineDoesNotPanic(t *testing.T) {
	path := writeSites(t, []config.Site{site("牵牛岗", 30.026, 119.007, 1489.9)})
	input := strings.Join([]string{
		"1", // 主菜单 [1]
		"1", // 步骤 1/6 模式 → 流星雨
		"1", // 步骤 2/6 日期 → 极大日 + 天数
		"2026-08-13",
		"0", // 往前推天数 = 0
		"1", // 步骤 3/6 点位 → 全部
		"",  // 步骤 4/6 导出 → 确认
		"",  // 步骤 5/6 高级 → 跳过
		"Y", // 步骤 6/6 确认 → 执行
		"",  // 执行后 pause
		"0", // 主菜单退出
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "执行内核未注入")
}

func TestReportFlowSunriseModeOmitsDouyinHonestly(t *testing.T) {
	// 新流程：主菜单[1] → 模式[2]日出 → 日期形式[1]锚点+天数 → 锚点 2026-08-15 → 天数\0
	// → 点位[1]全部 → 导出确认 → 高级跳过 → 确认[b]返回主菜单 → [0]退出。
	code, out := runMenu(t, "1\n2\n1\n2026-08-15\n\n1\n\n\nb\n0\n", nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out,
		"步骤 1/6：运行模式",
		"日出云海模式",
		"步骤 2/6：日期范围（日出云海）",
		"已确定观测夜：2026-08-14（日出当天 2026-08-15）",
		"步骤 4/6：导出内容",
		"抖音竖图：日出模式不支持",
		"步骤 5/6：高级选项",
	)
	// 日出模式不做抖音图，所以不应把它列为可勾选项（避免「选了又静默关闭」）。
	mustNotContain(t, out, "抖音竖版图")
}

func TestDouyinFlowWithoutReportsGivesFriendlyHint(t *testing.T) {
	code, out := runMenu(t, "2\nn\n0\n", nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out,
		"未在以下目录找到任何 astro_report_*.md",
		"请先执行 [1] 生成评估报告",
	)
}

func TestDouyinFlowRendersExistingReport(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "astro_report_peak-2026-08-13.md")
	body := "# 山地星空 · 低云海拔评估报告\n\n## 一、元信息\n"
	if err := os.WriteFile(mdPath, []byte(body), 0o644); err != nil {
		t.Fatalf("写测试报告失败：%v", err)
	}

	input := strings.Join([]string{
		"2",
		"1",
		"",
		"",
		"n",
		"Y",
		"",
		"0",
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) {
		o.Cfg.Output.OutDir = dir
		o.Cfg.Output.DouyinDir = filepath.Join(dir, "douyin")
	})
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out,
		"astro_report_peak-2026-08-13.md",
		"步骤 2/3：渲染小节",
		"点位列表", "天文条件", "核心窗口", "低云海拔评估明细",
		"共 2 张",
		"astro_report_peak-2026-08-13_sites.png",
	)
}

func TestSettingsFlowListsGroupsAndItems(t *testing.T) {
	_, out := runMenu(t, "4\nb\n0\n", nil)
	mustContain(t, out,
		"[4] 运行参数设置",
		"── 气象数据 ──", "── 时间窗口（北京时间） ──",
		"── 云 / 雾判据阈值 ──", "── 输出 ──",
		"气象模式", "输出目录", "默认天数", "报告后自动出图",
		"默认导出 CSV", "默认导出 JSON", "中文字体路径",
		"[a] 保存并返回", "[s] 保存到文件(留在本页)", "[o] 仅本次生效",
		"[r] 恢复默认值", "[b] 放弃修改并返回",
	)
}

func TestSettingsEditAndSave(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	newDir := filepath.Join(t.TempDir(), "myreports")
	input := strings.Join([]string{
		"4",
		"10",
		newDir,
		"s",
		"y",
		"b",
		"0",
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) { o.ConfigPath = cfgPath })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "已保存到 "+cfgPath)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("读回配置失败：%v", err)
	}
	var got config.Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("写出的 config.json 不可解析：%v", err)
	}
	if got.Output.OutDir != newDir {
		t.Errorf("out_dir = %q，期望 %q", got.Output.OutDir, newDir)
	}
}

func TestSettingsRejectsOutOfRangeThreshold(t *testing.T) {

	input := "4\n6\n150\n-1\n55\nb\no\n0\n"
	code, out := runMenu(t, input, nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "须在 0 ~ 100 之间", "云量阈值：40 % → 55 %")
}

func TestSettingsInvalidItemIndex(t *testing.T) {
	code, out := runMenu(t, "4\n999\nzzz\nb\n0\n", nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, `"999" 不是可选项`, `"zzz" 不是可选项`)
}

func TestValidateDate(t *testing.T) {
	cases := []struct {
		in      string
		wantErr string
	}{
		{"2026-08-13", ""},
		{"2024-02-29", ""},
		{"2026-02-29", "2026 年 2 月没有 29 日"},
		{"2026-02-30", "2026 年 2 月没有 30 日"},
		{"2026-13-01", "月份 13 不存在"},
		{"2026-00-10", "月份 0 不存在"},
		{"2026-08-32", "2026 年 8 月没有 32 日"},
		{"2026-8-13", "日期格式应为 YYYY-MM-DD"},

		{"2026/08/13", "日期格式应为 YYYY-MM-DD"},
		{"", "日期格式应为 YYYY-MM-DD"},
		{"abcd-ef-gh", "日期只能由数字与短横线组成"},
	}
	for _, c := range cases {
		_, err := ValidateDate(c.in)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("ValidateDate(%q) 报错 %v，期望通过", c.in, err)
		case c.wantErr != "" && err == nil:
			t.Errorf("ValidateDate(%q) 通过了，期望报错 %q", c.in, c.wantErr)
		case c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr):
			t.Errorf("ValidateDate(%q) = %v，期望包含 %q", c.in, err, c.wantErr)
		}
	}
}

func TestParseIndexSpec(t *testing.T) {
	cases := []struct {
		in      string
		n       int
		want    []int
		wantErr bool
	}{
		{"1,3,5", 12, []int{1, 3, 5}, false},
		{"1 3 5", 12, []int{1, 3, 5}, false},
		{"1-4", 12, []int{1, 2, 3, 4}, false},
		{"1,3-5 8", 12, []int{1, 3, 4, 5, 8}, false},
		{"4-1", 12, []int{1, 2, 3, 4}, false},
		{"3,3,3", 12, []int{3}, false},
		{"all", 3, []int{1, 2, 3}, false},
		{"none", 3, []int{}, false},
		{"", 3, []int{}, false},
		{"13", 12, nil, true},
		{"0", 12, nil, true},
		{"abc", 12, nil, true},
		{"1-", 12, nil, true},
	}
	for _, c := range cases {
		got, err := ParseIndexSpec(c.in, c.n)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseIndexSpec(%q,%d) 期望报错，实际 %v", c.in, c.n, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseIndexSpec(%q,%d) 报错 %v", c.in, c.n, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("ParseIndexSpec(%q,%d) = %v，期望 %v", c.in, c.n, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseIndexSpec(%q,%d) = %v，期望 %v", c.in, c.n, got, c.want)
				break
			}
		}
	}
}

func TestParseIndexSpecErrorMentionsRange(t *testing.T) {
	_, err := ParseIndexSpec("13", 12)
	if err == nil || !strings.Contains(err.Error(), "序号 13 超出范围（1~12）") {
		t.Errorf("错误信息 = %v，期望包含「序号 13 超出范围（1~12）」", err)
	}
}

func TestClipWidthKeepsDisplayWidth(t *testing.T) {
	cases := []struct {
		in  string
		max int
	}{
		{"短", 10},
		{"这是一段很长的中文备注需要被截断", 10},
		{"ASCII text that is fairly long", 12},
		{"混合mixed中文text", 8},
	}
	for _, c := range cases {
		got := ClipWidth(c.in, c.max)
		if w := dispWidthForTest(got); w > c.max {
			t.Errorf("ClipWidth(%q,%d) = %q，显示宽度 %d 超过上限", c.in, c.max, got, w)
		}
	}
}

func dispWidthForTest(s string) int {
	w := 0
	for _, r := range s {
		switch {
		case r == '\uFE0F':
		case r >= 0x1100 && r <= 0x115F,
			r >= 0x2E80 && r <= 0xA4CF,
			r >= 0xAC00 && r <= 0xD7A3,
			r >= 0xF900 && r <= 0xFAFF,
			r >= 0xFF01 && r <= 0xFF60,
			r >= 0x2600 && r <= 0x27BF,
			r >= 0x1F300 && r <= 0x1FAFF:
			w += 2
		default:
			w++
		}
	}
	return w
}

func TestBoxBordersAlign(t *testing.T) {
	_, out := runMenu(t, "0\n", nil)

	var boxLines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "┌") || strings.HasPrefix(line, "├") ||
			strings.HasPrefix(line, "└") || strings.HasPrefix(line, "│") {
			boxLines = append(boxLines, line)
		}
	}
	if len(boxLines) < 7 {
		t.Fatalf("只找到 %d 行方框内容，期望 ≥7（上边框+标题+分隔+4 行状态+下边框）", len(boxLines))
	}
	want := dispWidthForTest(boxLines[0])
	for i, line := range boxLines {
		if got := dispWidthForTest(line); got != want {
			t.Errorf("方框第 %d 行显示宽度 = %d，期望 %d\n  %s", i+1, got, want, line)
		}
	}
}

func TestSiteTableColumnsAlign(t *testing.T) {

	path := writeSites(t, []config.Site{
		site("牵牛岗", 30.0260, 119.0070, 1489.9),
		site("A", 1.5, 2.5, 3.5),
		site("十六个字符的超长点位名称测试用", 45.0, 90.0, 800.0),
	})
	_, out := runMenu(t, "3\nb\n0\n", func(o *Options) { o.SitesPath = path })

	var dataLines []string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		for _, seq := range []string{"1  ", "2  ", "3  "} {
			if strings.HasPrefix(trimmed, seq) && strings.Contains(line, "  是") {
				dataLines = append(dataLines, line)
				break
			}
		}
	}
	if len(dataLines) != 3 {
		t.Fatalf("匹配到 %d 行点位数据，期望 3\n%s", len(dataLines), out)
	}

	firstCol := -1
	for i, line := range dataLines {
		idx := strings.LastIndex(line, "是")
		if idx < 0 {
			t.Fatalf("第 %d 行找不到启用列：%q", i+1, line)
		}
		col := dispWidthForTest(line[:idx])
		if firstCol < 0 {
			firstCol = col
		} else if col != firstCol {
			t.Errorf("第 %d 行「启用」列起始于第 %d 显示列，期望 %d（表格错位）\n  %s",
				i+1, col, firstCol, line)
		}
	}
}
