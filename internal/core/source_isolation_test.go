package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/dualtrack"
)

const tomorrowPkg = "github.com/prophetcro/astro-mountain/internal/api/tomorrow"

const meteobluePkg = "github.com/prophetcro/astro-mountain/internal/api/meteoblue"

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("拿不到当前测试文件路径，无法定位仓库根目录")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
}

func depsOf(t *testing.T, pkg string) depsList {
	t.Helper()
	if testing.Short() {
		t.Skip("-short：跳过需要调用 go 工具链的依赖图断言")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("找不到 go 工具链，跳过依赖图断言：%v", err)
	}

	cmd := exec.Command(goBin, "list", "-deps", pkg)
	cmd.Dir = moduleRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps %s 执行失败：%v\n%s", pkg, err, out)
	}

	deps := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(deps) < 10 {
		t.Fatalf("%s 的依赖图只有 %d 项，go list 可能没正常工作：\n%s",
			pkg, len(deps), out)
	}
	return deps
}

func dependsOnTomorrow(deps []string) bool {
	for _, d := range deps {
		if strings.TrimSpace(d) == tomorrowPkg {
			return true
		}
	}
	return false
}

func TestTomorrowClientStaysInCompositionRoot(t *testing.T) {

	if !dependsOnTomorrow(depsOf(t, "./cmd/astro-mountain")) {
		t.Errorf("组合根 cmd/astro-mountain 的依赖图里**没有** %s，"+
			"说明 B 轨取数器没接线。\n"+
			"此时 engine.TomorrowWired() 恒为 false，--source tomorrow 会被"+
			"启动期闸门一律拒绝，B 轨等于报废。\n"+
			"接线点在 cmd/astro-mountain/main.go 的 cli.TomorrowFetcherFactory。",
			tomorrowPkg)
	}

	midLayers := []struct {
		pkg string
		why string
	}{
		{"./internal/core", "core 是每条代码路径都会碰的编排层，" +
			"它只该认识数据源的名字（Source 枚举）与中立接口（TomorrowFetcher），" +
			"不该认识厂商客户端本身"},
		{"./internal/cli", "cli 走的是 cmd → cli → core 这条必经之路；" +
			"注入点必须留在 main，靠 cli.TomorrowFetcherFactory 这个函数变量" +
			"把依赖方向反转"},
		{"./internal/menu", "菜单只消费 core.Engine，" +
			"它连数据源怎么取数都不该知道"},
	}
	for _, m := range midLayers {
		if dependsOnTomorrow(depsOf(t, m.pkg)) {
			t.Errorf("%s 的依赖图里出现了 %s —— 注入点下沉了。\n原因：%s。\n"+
				"请改回在 cmd/astro-mountain/main.go 注册 cli.TomorrowFetcherFactory。",
				m.pkg, tomorrowPkg, m.why)
		}
	}
}

func TestCoreDoesNotDependOnVendorAPIPackages(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过需要调用 go 工具链的依赖图断言")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("找不到 go 工具链，跳过依赖图断言：%v", err)
	}

	cmd := exec.Command(goBin, "list", "-deps", "./internal/core")
	cmd.Dir = moduleRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps 执行失败：%v\n%s", err, out)
	}

	for _, d := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(d) == tomorrowPkg {
			t.Fatalf("internal/core 依赖了 %s。core 只该认识数据源的**名字**"+
				"（Source 枚举），不该认识厂商客户端本身；否则每条代码路径都会"+
				"把 B 轨客户端拖进二进制。", tomorrowPkg)
		}
		if strings.TrimSpace(d) == meteobluePkg {
			t.Fatalf("internal/core 依赖了 %s。core 只该认识数据源的**名字**"+
				"（Source 枚举）与中立接口（MeteoblueFetcher），不该认识厂商客户端本身；"+
				"否则每条代码路径都会把 C 轨客户端拖进二进制。", meteobluePkg)
		}
	}
}

func TestMeteoblueClientStaysInCompositionRoot(t *testing.T) {
	if !depsOf(t, "./cmd/astro-mountain").containsPkg(meteobluePkg) {
		t.Errorf("组合根 cmd/astro-mountain 的依赖图里**没有** %s，"+
			"说明 C 轨取数器没接线。\n"+
			"此时 engine.MeteoblueWired() 恒为 false，--source meteoblue 会被"+
			"启动期闸门一律拒绝，C 轨等于报废。\n"+
			"接线点在 cmd/astro-mountain/main.go 的 cli.MeteoblueFetcherFactory。",
			meteobluePkg)
	}

	midLayers := []struct {
		pkg string
		why string
	}{
		{"./internal/core", "core 是每条代码路径都会碰的编排层，" +
			"它只该认识数据源的名字（Source 枚举）与中立接口（MeteoblueFetcher），" +
			"不该认识厂商客户端本身"},
		{"./internal/cli", "cli 走的是 cmd → cli → core 这条必经之路；" +
			"注入点必须留在 main，靠 cli.MeteoblueFetcherFactory 这个函数变量" +
			"把依赖方向反转"},
		{"./internal/menu", "菜单只消费 core.Engine，" +
			"它连数据源怎么取数都不该知道"},
	}
	for _, m := range midLayers {
		if depsOf(t, m.pkg).containsPkg(meteobluePkg) {
			t.Errorf("%s 的依赖图里出现了 %s —— 注入点下沉了。\n原因：%s。\n"+
				"请改回在 cmd/astro-mountain/main.go 注册 cli.MeteoblueFetcherFactory。",
				m.pkg, meteobluePkg, m.why)
		}
	}
}

// depsList 是 go list -deps 输出的小包装，便于做包名包含判断。
type depsList []string

func (d depsList) containsPkg(pkg string) bool {
	for _, x := range d {
		if strings.TrimSpace(x) == pkg {
			return true
		}
	}
	return false
}

func TestTomorrowReportWiredTripwire(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过需要编译主二进制并查符号表的绊线断言")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("找不到 go 工具链，跳过绊线断言：%v", err)
	}

	binPath := filepath.Join(t.TempDir(), "astro-mountain")
	build := exec.Command(goBin, "build", "-o", binPath, "./cmd/astro-mountain")
	build.Dir = moduleRoot(t)
	if out, berr := build.CombinedOutput(); berr != nil {
		t.Fatalf("编译主二进制失败，无法查符号表：%v\n%s", berr, out)
	}

	nm := exec.Command(goBin, "tool", "nm", binPath)
	nmOut, nerr := nm.CombinedOutput()
	if nerr != nil {
		t.Fatalf("go tool nm 失败：%v\n%s", nerr, nmOut)
	}

	if !binaryHasSymbol(string(nmOut),
		"github.com/prophetcro/astro-mountain/internal/dualtrack.Assemble") {
		t.Errorf("绊线触发：主二进制里**没有** dualtrack.Assemble 符号——" +
			"B 轨评级函数被链接器当死代码消掉了。\n" +
			"成因：Assemble 的唯一调用链是 Engine.Run 的 `if useTomorrow` 分支 → " +
			"runTomorrowTrack → Assemble。符号消失说明这条链在某处断了" +
			"（分支被删 / 被注释 / 条件恒假）。\n" +
			"后果：--source tomorrow 会通过启动期闸门，然后跑出一份 A 轨报告——" +
			"D4-6 红线第 4 条的原样复现，也正是 2026-08-07 那个 P0 的现场。\n" +
			"处理：把 Engine.Run 里的 B 轨分支接回去。**不要**通过删除本断言来修绿。")
	}
}

func TestTomorrowScaffoldConstIsGone(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(moduleRoot(t),
		"internal", "core", "tomorrow_track.go"))
	if err != nil {
		t.Fatalf("读取 tomorrow_track.go 失败：%v", err)
	}

	for i, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.Contains(trimmed, "tomorrowReportWired") {
			t.Errorf("tomorrow_track.go:%d 出现了脚手架标识符 tomorrowReportWired：\n\t%s\n"+
				"它是个编译期字面量，却在断言一件运行期的事（B 轨能不能交付）。"+
				"T6 已经把它删除，判据回到唯一的运行期来源 TomorrowFetcher != nil。\n"+
				"若你是想表达某种新的不可交付状态，请让判据读运行期字段，"+
				"不要再引入常量——它当天能工作，之后每天都在说谎。", i+1, trimmed)
		}
	}

	for _, tc := range []struct {
		name  string
		eng   *Engine
		wired bool
	}{
		{"未注入取数器", &Engine{}, false},
		{"已注入取数器", &Engine{TomorrowFetcher: stubTomorrowFetcher{}}, true},
	} {
		if got := tc.eng.TomorrowWired(); got != tc.wired {
			t.Errorf("%s：TomorrowWired() = %v，期望 %v", tc.name, got, tc.wired)
		}
		if got, want := tc.eng.TomorrowDeliverable(), tc.eng.TomorrowWired(); got != want {
			t.Errorf("%s：TomorrowDeliverable() = %v，但 TomorrowWired() = %v。"+
				"脚手架删除后两者必须同值；若你新加了交付条件，"+
				"请确认它读的是运行期状态而不是编译期常量。", tc.name, got, want)
		}
	}
}

type stubTomorrowFetcher struct{}

func (stubTomorrowFetcher) Name() string { return "stub" }

func (stubTomorrowFetcher) FetchSite(ctx context.Context, site Site) (
	[]dualtrack.HourInput, string, bool, error) {
	return nil, dualtrack.DatumAGL, true, nil
}

func binaryHasSymbol(nmOutput, symbol string) bool {
	for _, line := range strings.Split(nmOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[len(fields)-1] == symbol {
			return true
		}
	}
	return false
}

func TestMeteoblueReportWiredTripwire(t *testing.T) {
	if testing.Short() {
		t.Skip("-short：跳过需要编译主二进制并查符号表的绊线断言")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("找不到 go 工具链，跳过绊线断言：%v", err)
	}

	binPath := filepath.Join(t.TempDir(), "astro-mountain")
	build := exec.Command(goBin, "build", "-o", binPath, "./cmd/astro-mountain")
	build.Dir = moduleRoot(t)
	if out, berr := build.CombinedOutput(); berr != nil {
		t.Fatalf("编译主二进制失败，无法查符号表：%v\n%s", berr, out)
	}

	nm := exec.Command(goBin, "tool", "nm", binPath)
	nmOut, nerr := nm.CombinedOutput()
	if nerr != nil {
		t.Fatalf("go tool nm 失败：%v\n%s", nerr, nmOut)
	}

	// 取数器构造器 meteoblue.New 的符号，仅当 main 注册了 cli.MeteoblueFetcherFactory
	// 把 api/meteoblue 链入主二进制时才出现；它是 C 轨接线与否的最直接静态证据。
	const meteoblueNewSymbol = "github.com/prophetcro/astro-mountain/internal/api/meteoblue.New"
	if !binaryHasSymbol(string(nmOut), meteoblueNewSymbol) {
		t.Errorf("绊线触发：主二进制里**没有** %s 符号——"+
			"C 轨取数器被链接器当死代码消掉了，或 main 没注册 cli.MeteoblueFetcherFactory。\n"+
			"成因：--source meteoblue 的整条取数链（Engine.Run 的 useMeteoblue 分支 → "+
			"MeteoblueFetcher.FetchSite → api/meteoblue）在某处断了。\n"+
			"后果：--source meteoblue 会通过启动期闸门，然后回落到 Open-Meteo 报告——"+
			"用户以为在看 Meteoblue、实际是 A 轨。\n"+
			"处理：把 C 轨接线接回（main 注册 cli.MeteoblueFetcherFactory 指向 meteoblue.New）。"+
			"**不要**通过删除本断言来修绿。", meteoblueNewSymbol)
	}
}
