package menu

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
)

const aTrackLabel = "Open-Meteo"

func TestAdvancedMenuRejectsUnavailableSource(t *testing.T) {

	t.Setenv("TOMORROW_API_KEY", "")

	_, out := runMenu(t, "1\n1\n\n\n\n\n1\ntomorrow\n\n\nb\nq\ny\n", nil)

	mustContain(t, out, "未采用", "Tomorrow.io", "本版不可用", "请改选其他数据源")

	mustContain(t, out, "数据源", aTrackLabel)
	mustNotContain(t, out, "--source tomorrow")
}

func TestSourceListMarksUnavailableBeforeChoosing(t *testing.T) {
	t.Setenv("TOMORROW_API_KEY", "")

	_, out := runMenu(t, "1\n1\n\n\n\n\n1\n\n\nb\nq\ny\n", nil)

	mustContain(t, out, "本版不可用")

	mustContain(t, out, "500", "25", "3")

	mustNotContain(t, out, "--source tomorrow")
}

func TestExecuteRefusesUnavailableSource(t *testing.T) {
	t.Setenv("TOMORROW_API_KEY", "")

	cfg := config.Default()
	cfg.Output.OutDir = t.TempDir()

	var out bytes.Buffer
	s := &state{
		cfg: cfg,
		ctx: context.Background(),

		u: newUI(context.Background(), strings.NewReader("\n"), &out),
	}

	f := &reportForm{
		usePeak:      true,
		peak:         "2026-08-12",
		days:         3,
		source:       core.SourceTomorrow,
		models:       cfg.API.Models,
		wantMarkdown: true,
		allSites:     true,
	}

	if err := s.execute(f); err != nil {
		t.Fatalf("execute 不该返回错误（应是「拒绝并暂停」）：%v", err)
	}

	got := out.String()
	if !strings.Contains(got, "无法执行") {
		t.Errorf("应明确拒绝执行，实际输出：\n%s", got)
	}
	if !strings.Contains(got, "未生成任何报告") {
		t.Errorf("应说明没有产出任何报告，实际输出：\n%s", got)
	}

	if strings.Contains(got, "执行内核未注入") {
		t.Errorf("Engine 判空跑到了数据源闸前面——" +
			"这会让本条闸在内核已注入的真实环境里变成唯一防线，且无法被无网测试覆盖")
	}
}

func TestSourceMenuLineLeavesAvailableSourceClean(t *testing.T) {
	t.Setenv("TOMORROW_API_KEY", "")

	s := newTestState(t)

	openMeteo := s.sourceMenuLine(core.SourceOpenMeteo)
	if strings.Contains(openMeteo, "本版不可用") {
		t.Errorf("A 轨可用，不该被标注不可用：%q", openMeteo)
	}

	if !strings.Contains(openMeteo, aTrackLabel) || !strings.Contains(openMeteo, "默认") {
		t.Errorf("A 轨那行丢了标签或「默认」标记：%q", openMeteo)
	}

	tomorrow := s.sourceMenuLine(core.SourceTomorrow)
	if !strings.Contains(tomorrow, "本版不可用") {
		t.Errorf("B 轨当前不可用，列表里必须标出来：%q", tomorrow)
	}
	if !strings.Contains(tomorrow, "Tomorrow.io") {
		t.Errorf("B 轨那行丢了标签：%q", tomorrow)
	}
}
