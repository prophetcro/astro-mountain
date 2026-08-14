package menu

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
)

func newBackTestUI(input string) (*ui, *bytes.Buffer) {
	var out bytes.Buffer
	return newUI(context.Background(), strings.NewReader(input), &out), &out
}

func TestPromptBackReturnsErrBackInListMode(t *testing.T) {
	for _, key := range []string{"b", "B", "back", "BACK"} {
		t.Run("key="+key, func(t *testing.T) {
			u, _ := newBackTestUI(key + "\n")
			got, err := u.prompt("请选择", "", backReturns)
			if !errors.Is(err, errBack) {
				t.Fatalf("prompt(%q, backReturns) 的 err = %v，期望 errBack", key, err)
			}
			if got != "" {
				t.Errorf("prompt(%q, backReturns) 的返回值 = %q，期望空串", key, got)
			}
		})
	}
}

func TestPromptBackIsLiteralInStayMode(t *testing.T) {
	for _, key := range []string{"b", "B", "back"} {
		t.Run("key="+key, func(t *testing.T) {
			u, _ := newBackTestUI(key + "\n")
			got, err := u.prompt("如何处理？", "y", backLiteral)
			if err != nil {
				t.Fatalf("prompt(%q, backLiteral) 不该返回错误，实际 %v", key, err)
			}
			if got != key {
				t.Errorf("prompt(%q, backLiteral) 的返回值 = %q，期望原样返回 %q", key, got, key)
			}
		})
	}
}

func TestPromptQuitStillWorksInStayMode(t *testing.T) {
	u, _ := newBackTestUI("q\ny\n")
	if _, err := u.prompt("如何处理？", "y", backLiteral); !errors.Is(err, errQuit) {
		t.Fatalf("backLiteral 下输入 q 的 err = %v，期望 errQuit", err)
	}
}

func TestChoiceBackReturnsErrBackInListMode(t *testing.T) {

	u, _ := newBackTestUI("b\n")
	got, err := u.choice("请选择", []string{"a", "s", "b"}, "b", backReturns)
	if !errors.Is(err, errBack) {
		t.Fatalf("choice(backReturns) 输入 b 的 err = %v，期望 errBack", err)
	}
	if got != "" {
		t.Errorf("choice(backReturns) 输入 b 的返回值 = %q，期望空串", got)
	}
}

func TestChoiceBackIsSelectableInStayMode(t *testing.T) {
	u, out := newBackTestUI("b\n")
	got, err := u.choice("现在保存到文件吗？", []string{"y", "s", "n", "b"}, "y", backLiteral)
	if err != nil {
		t.Fatalf("choice(backLiteral) 输入 b 不该返回错误，实际 %v", err)
	}
	if got != "b" {
		t.Errorf("choice(backLiteral) 输入 b 的返回值 = %q，期望 %q", got, "b")
	}
	if strings.Contains(out.String(), "不是可选项") {
		t.Errorf("b 被误判为非法输入\n---- 实际输出 ----\n%s", out.String())
	}
}

func TestLeaveSettingsBackStaysOnPage(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	newDir := filepath.Join(t.TempDir(), "myreports")
	input := strings.Join([]string{
		"4",
		"10",
		newDir,
		"b",
		"b",
		"b",
		"o",
		"0",
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) { o.ConfigPath = cfgPath })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "b=留在本页", "已留在参数设置页")

	if n := countBanner(out, "[4] 运行参数设置"); n != 3 {
		t.Errorf("设置页渲染 %d 次，期望 3（离开确认按 b 应留在本页）\n---- 实际输出 ----\n%s", n, out)
	}

	mustNotContain(t, out, "已保存到 "+cfgPath)
}

func TestLeaveSiteManagerBackStaysOnPage(t *testing.T) {
	path := writeSites(t, []config.Site{
		site("牵牛岗", 30.026, 119.007, 1489.9),
		site("括苍山", 28.8101, 120.9221, 1382.6),
	})
	input := strings.Join([]string{
		"3",
		"t", "2",
		"b",
		"b",
		"b",
		"n",
		"0",
		"y",
	}, "\n") + "\n"

	code, out := runMenu(t, input, func(o *Options) { o.SitesPath = path })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "b=留在本页", "已留在点位配置管理页")

	if n := countBanner(out, "[3] 点位配置管理"); n != 3 {
		t.Errorf("点位页渲染 %d 次，期望 3（离开确认按 b 应留在本页）\n---- 实际输出 ----\n%s", n, out)
	}

	res, err := config.LoadSites(path)
	if err != nil {
		t.Fatalf("重新加载失败：%v", err)
	}
	if len(res.Enabled()) != 2 {
		t.Errorf("未保存却写盘了：文件里的启用点位数 = %d，期望仍为 2", len(res.Enabled()))
	}
}

func TestSettingsListBackStillReturnsToMainMenu(t *testing.T) {

	code, out := runMenu(t, "4\nb\n0\n", nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	if n := countBanner(out, "[4] 运行参数设置"); n != 1 {
		t.Errorf("设置页渲染 %d 次，期望 1（列表页按 b 应返回主菜单）\n---- 实际输出 ----\n%s", n, out)
	}
	mustNotContain(t, out, "运行参数有未保存的修改。", "已留在参数设置页")
}

func TestSiteListBackStillReturnsToMainMenu(t *testing.T) {
	path := writeSites(t, []config.Site{site("牵牛岗", 30.026, 119.007, 1489.9)})
	code, out := runMenu(t, "3\nb\n0\n", func(o *Options) { o.SitesPath = path })
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	if n := countBanner(out, "[3] 点位配置管理"); n != 1 {
		t.Errorf("点位页渲染 %d 次，期望 1（列表页按 b 应返回主菜单）\n---- 实际输出 ----\n%s", n, out)
	}
	mustNotContain(t, out, "有未保存的点位修改", "已留在点位配置管理页")
}

func TestSettingsListBackWithDirtyGoesThroughConfirm(t *testing.T) {
	newDir := filepath.Join(t.TempDir(), "myreports")
	input := strings.Join([]string{"4", "10", newDir, "b", "o", "0"}, "\n") + "\n"
	code, out := runMenu(t, input, nil)
	if code != 0 {
		t.Fatalf("退出码 = %d，期望 0", code)
	}
	mustContain(t, out, "运行参数有未保存的修改。", "已保留在内存中，仅本次运行生效")
	if n := countBanner(out, "[4] 运行参数设置"); n != 2 {
		t.Errorf("设置页渲染 %d 次，期望 2（确认页选 o 应回主菜单）\n---- 实际输出 ----\n%s", n, out)
	}
}

func TestMainMenuBackQuitsCleanly(t *testing.T) {
	code, out := runMenu(t, "b\n", nil)
	if code != 0 {
		t.Fatalf("主菜单按 b 的退出码 = %d，期望 0", code)
	}
	mustNotContain(t, out, "菜单异常退出")
}
