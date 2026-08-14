package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
)

func countBanner(out, title string) int {
	return strings.Count(out, "══ "+title+" ")
}

func readConfigFile(t *testing.T, path string) config.Config {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回配置失败：%v", err)
	}
	var got config.Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("写出的 config.json 不可解析：%v", err)
	}
	return got
}

func TestSettingsSaveAndReturnWithA(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	newDir := filepath.Join(t.TempDir(), "myreports")

	input := strings.Join([]string{
		"4",
		"10",
		newDir,
		"a",
		"y",
		"0",
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) { o.ConfigPath = cfgPath })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "已保存到 "+cfgPath)

	if n := countBanner(out, "[4] 运行参数设置"); n != 2 {
		t.Errorf("设置页渲染 %d 次，期望 2（[a] 应在保存后直接返回主菜单）\n---- 实际输出 ----\n%s", n, out)
	}

	if got := readConfigFile(t, cfgPath).Output.OutDir; got != newDir {
		t.Errorf("out_dir = %q，期望 %q", got, newDir)
	}
}

func TestSettingsSaveAndReturnWithAWithoutPendingEdits(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	input := "4\na\ny\n0\n"

	code, out := runMenu(t, input, func(o *Options) { o.ConfigPath = cfgPath })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "已保存到 "+cfgPath)
	if n := countBanner(out, "[4] 运行参数设置"); n != 1 {
		t.Errorf("设置页渲染 %d 次，期望 1", n)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config.json 未写出：%v", err)
	}
}

func TestSettingsSaveAndReturnCancelDoesNotDropEdits(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	newDir := filepath.Join(t.TempDir(), "myreports")

	input := strings.Join([]string{
		"4", "10", newDir,
		"a",
		"n",
		"o",
		"0",
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) { o.ConfigPath = cfgPath })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out,
		"已取消保存",
		"运行参数有未保存的修改。",
		"已保留在内存中，仅本次运行生效",
	)

	if _, err := os.Stat(cfgPath); err == nil {
		t.Errorf("用户取消了保存，却写出了 %s", cfgPath)
	}
}

func TestSettingsSaveStayKeepsUserOnPage(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	newDir := filepath.Join(t.TempDir(), "myreports")

	input := strings.Join([]string{
		"4", "10", newDir,
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

	if n := countBanner(out, "[4] 运行参数设置"); n != 3 {
		t.Errorf("设置页渲染 %d 次，期望 3（[s] 应留在本页）\n---- 实际输出 ----\n%s", n, out)
	}

	mustNotContain(t, out, "运行参数有未保存的修改。")
}

func TestLeaveSettingsSaveKeyAcceptsBothYAndS(t *testing.T) {
	for _, key := range []string{"y", "s"} {
		t.Run("key="+key, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "config.json")
			newDir := filepath.Join(t.TempDir(), "myreports")
			input := strings.Join([]string{
				"4", "10", newDir,
				"b",
				key,
				"y",
				"0",
			}, "\n") + "\n"

			code, out := runMenu(t, input, func(o *Options) { o.ConfigPath = cfgPath })
			if code != 0 {
				t.Fatalf("退出码 = %d，期望 0", code)
			}
			mustContain(t, out,
				"y/s=保存并返回",
				"已保存到 "+cfgPath,
			)

			mustNotContain(t, out, "不是可选项，请输入 y / s / o / d / b 之一")

			if got := readConfigFile(t, cfgPath).Output.OutDir; got != newDir {
				t.Errorf("out_dir = %q，期望 %q", got, newDir)
			}
		})
	}
}

func TestLeaveSettingsDiscardStillWorks(t *testing.T) {

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	original := config.Default()
	original.Output.OutDir = filepath.Join(t.TempDir(), "original")
	if err := config.Save(original, cfgPath); err != nil {
		t.Fatalf("准备 config.json 失败：%v", err)
	}

	newDir := filepath.Join(t.TempDir(), "myreports")

	input := strings.Join([]string{
		"4", "10", newDir,
		"b",
		"d",
		"0",
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) {
		o.ConfigPath = cfgPath
		o.Cfg = original
	})
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "已放弃修改，恢复为文件中的值")

	if got := readConfigFile(t, cfgPath).Output.OutDir; got != original.Output.OutDir {
		t.Errorf("out_dir = %q，期望保持 %q（用户选了放弃修改）", got, original.Output.OutDir)
	}
}

func TestSiteManagerSaveAndReturnWithW(t *testing.T) {
	path := writeSites(t, []config.Site{
		site("牵牛岗", 30.026, 119.007, 1489.9),
		site("括苍山", 28.8101, 120.9221, 1382.6),
	})
	input := strings.Join([]string{
		"3",
		"t",
		"2",
		"w",
		"y",
		"0",
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "[w] 保存并返回", "已保存 2 个点位到 "+path)
	if n := countBanner(out, "[3] 点位配置管理"); n != 2 {
		t.Errorf("点位页渲染 %d 次，期望 2（[w] 应在保存后直接返回主菜单）\n---- 实际输出 ----\n%s", n, out)
	}

	mustNotContain(t, out, "有未保存的点位修改")

	res, err := config.LoadSites(path)
	if err != nil {
		t.Fatalf("重新加载失败：%v", err)
	}
	if len(res.Enabled()) != 1 {
		t.Errorf("停用一个后启用点位数 = %d，期望 1", len(res.Enabled()))
	}
}

func TestSiteManagerSaveAndReturnCancelDoesNotDropEdits(t *testing.T) {
	path := writeSites(t, []config.Site{
		site("牵牛岗", 30.026, 119.007, 1489.9),
		site("括苍山", 28.8101, 120.9221, 1382.6),
	})
	input := strings.Join([]string{
		"3", "t", "2",
		"w",
		"n",
		"n",
		"0", "y",
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "已取消保存", "有未保存的点位修改")

	res, _ := config.LoadSites(path)
	if len(res.Enabled()) != 2 {
		t.Errorf("取消保存后文件里的启用点位数 = %d，期望仍为 2", len(res.Enabled()))
	}
}

func TestLeaveSiteManagerSaveKeyAcceptsBothYAndS(t *testing.T) {
	for _, key := range []string{"y", "s"} {
		t.Run("key="+key, func(t *testing.T) {
			path := writeSites(t, []config.Site{
				site("牵牛岗", 30.026, 119.007, 1489.9),
				site("括苍山", 28.8101, 120.9221, 1382.6),
			})
			input := strings.Join([]string{
				"3", "t", "2",
				"b",
				key,
				"y",
				"0",
			}, "\n") + "\n"

			code, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
			if code != 0 {
				t.Fatalf("退出码 = %d，期望 0", code)
			}
			mustContain(t, out, "y/s=保存并返回", "已保存 2 个点位到 "+path)
			mustNotContain(t, out, "不是可选项，请输入 y / s / n / b 之一")

			res, err := config.LoadSites(path)
			if err != nil {
				t.Fatalf("重新加载失败：%v", err)
			}
			if len(res.Enabled()) != 1 {
				t.Errorf("停用一个后启用点位数 = %d，期望 1", len(res.Enabled()))
			}
		})
	}
}
