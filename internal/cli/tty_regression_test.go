package cli

import (
	"os"
	"testing"
)

func TestStdinIsTTY_DevNullIsNotATerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("打开 %s 失败：%v", os.DevNull, err)
	}
	defer devNull.Close()

	orig := os.Stdin
	os.Stdin = devNull
	defer func() { os.Stdin = orig }()

	if StdinIsTTY() {
		t.Errorf("StdinIsTTY() = true，期望 false：" +
			"/dev/null 是字符设备但不是终端，cron 正是用它作为 stdin。" +
			"误判为 TTY 会让无业务参数的定时任务弹出菜单并静默空跑（退出码仍为 0）。")
	}
}

func TestStdinIsTTY_RegularFileIsNotATerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatalf("创建临时文件失败：%v", err)
	}
	defer f.Close()

	orig := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = orig }()

	if StdinIsTTY() {
		t.Errorf("StdinIsTTY() = true，期望 false：普通文件重定向不是终端")
	}
}

func TestImplicitBatch_DevNullShouldRunBatch(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("打开 %s 失败：%v", os.DevNull, err)
	}
	defer devNull.Close()

	orig := os.Stdin
	os.Stdin = devNull
	defer func() { os.Stdin = orig }()

	opts, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse(nil) 失败：%v", err)
	}

	isTTY := StdinIsTTY()
	if opts.ShouldEnterMenu(isTTY) {
		t.Errorf("ShouldEnterMenu = true，期望 false："+
			"stdin=/dev/null（cron 标准行为）应直接执行默认任务。"+
			"当前 StdinIsTTY() 返回 %v，导致进入交互菜单后立刻 EOF 退出，"+
			"不产出任何报告却仍返回退出码 0。", isTTY)
	}
}
