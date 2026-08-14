package render

import (
	"fmt"
	"testing"
)

func measureCoreWindow(t *testing.T, sites []string) {
	md := "## 2.2 核心窗口\n\n| 点位 | 结论 |\n|---|---|\n"
	for _, s := range sites {
		md += fmt.Sprintf("| %s | ✅通透 |\n", s)
	}
	lines := splitLines(md)
	secs := ParseSections(lines)
	sec := secs[0]
	blocks := ParseBlocks(lines[sec.Start+1 : sec.End])
	title, _ := ComposeOutputMeta(sec, sec, "核心窗口")
	pages := PaginateSection(title, blocks, "核心窗口")

	var totalRows int
	for _, b := range blocks {
		if b.Kind == BlockTable {
			totalRows = len(b.Rows)
		}
	}
	if pages == nil {

		_, bottom := measureSection(title, blocks, 1.0)
		pct := float64(bottom) / float64(BottomLimit) * 100
		t.Logf("[核心窗口] 单页 | 总数据行=%d | 底部y=%d (%.1f%% of %d) | scale=1.0",
			totalRows, bottom, pct, BottomLimit)
		return
	}
	t.Logf("[核心窗口] 页数=%d | 总数据行=%d", len(pages), totalRows)
	for i, p := range pages {
		var rows int
		for _, b := range p.Blocks {
			if b.Kind == BlockTable {
				rows = len(b.Rows)
			}
		}
		_, bottom := measureSection(p.Title, p.Blocks, 1.0)
		pct := float64(bottom) / float64(BottomLimit) * 100
		t.Logf("  page[%d] 行数=%d 底部y=%d (%.1f%%) scale=1.0", i+1, rows, bottom, pct)
	}
}

func measureCloudDetail(t *testing.T, rows int) {
	md := "## 四、低云海拔评估明细\n\n### 2026-08-12 夜\n\n" +
		"| 点位 | 海拔m | 有效h | 通透h | 风险h | 不宜h | 窗口 | 云底 | 云顶 | 状态 | 结论 |\n" +
		"|---|---|---|---|---|---|---|---|---|---|---|\n"
	for i := 0; i < rows; i++ {
		md += fmt.Sprintf("| 点位%d | 1650 | 6/6 | 6 | 0 | 0 | 23:00-05:00 | -800 | -300 | 云海在脚下 | ✅通透：整夜云海在脚下，可拍 |\n", i)
	}
	lines := splitLines(md)
	secs := ParseSections(lines)
	parent := secs[0]
	var target Section
	for _, s := range secs {
		if NightDateOf(s) != "" {
			target = s
			break
		}
	}
	blocks := ParseBlocks(lines[target.Start+1 : target.End])
	title, _ := ComposeOutputMeta(parent, target, "低云海拔评估明细")
	pages := PaginateSection(title, blocks, "低云海拔评估明细")
	if pages == nil {
		_, bottom := measureSection(title, blocks, 1.0)
		pct := float64(bottom) / float64(BottomLimit) * 100
		t.Logf("[低云海拔评估明细 %d行] 单页 | 底部y=%d (%.1f%%) scale=1.0", rows, bottom, pct)
		return
	}
	t.Logf("[低云海拔评估明细 %d行] 页数=%d", rows, len(pages))
	for i, p := range pages {
		var nrows int
		for _, b := range p.Blocks {
			if b.Kind == BlockTable {
				nrows = len(b.Rows)
			}
		}
		_, bottom := measureSection(p.Title, p.Blocks, 1.0)
		pct := float64(bottom) / float64(BottomLimit) * 100
		t.Logf("  page[%d] 行数=%d 底部y=%d (%.1f%%) scale=1.0", i+1, nrows, bottom, pct)
	}
}

func TestMeasureFillBeforeAfter(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	requireFont(t)
	sites13 := []string{
		"牵牛岗", "太子尖", "百丈岭", "饭甑尖", "梅干岭", "天荒坪", "安顶山",
		"四明山", "青梅尖", "括苍山", "星辰山", "牛草山", "冷湖镇",
	}
	sites30 := []string{
		"牵牛岗", "太子尖", "百丈岭", "饭甑尖", "梅干岭", "天荒坪", "安顶山",
		"四明山", "青梅尖", "括苍山", "星辰山", "牛草山", "冷湖镇",
		"大明山", "华顶峰", "黄茅尖", "清凉峰", "天目山", "龙王山",
		"莫干山", "会稽山", "千里岗", "龙门山", "雪窦山", "大盘山",
		"九龙山", "白马尖", "莲花峰", "东白山", "云和梯田",
	}
	t.Log("===== 测量开始（自适应装箱：scale=1.0 下每页贪心塞满）=====")
	t.Log("--- 核心窗口 13 机位（2 列矮行）---")
	measureCoreWindow(t, sites13)
	t.Log("--- 核心窗口 30 机位（2 列矮行，超过单页容量→应自适应多页且每页塞满）---")
	measureCoreWindow(t, sites30)
	t.Log("--- 低云海拔评估明细 12 行（10 列高行）---")
	measureCloudDetail(t, 12)
	t.Log("--- 低云海拔评估明细 24 行（10 列高行，压力）---")
	measureCloudDetail(t, 24)
}
