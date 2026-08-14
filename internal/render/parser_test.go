package render

import (
	"strings"
	"testing"
)

func TestParseSections(t *testing.T) {
	lines := strings.Split(strings.Join([]string{
		"# 一级标题（parse_sections 只收 ## 起，应忽略）",
		"",
		"## 一、元信息",
		"",
		"### 1.1 点位列表",
		"",
		"| 点位 | 海拔 |",
		"| --- | --- |",
		"| 牵牛岗 | 1650 |",
		"",
		"## 二、汇总",
		"",
		"### 2.1 天文条件（近似算法）",
		"",
		"正文。",
		"",
		"### 2026-08-12 夜（2026-08-12 22:00 → 次日 06:00）",
		"",
		"## 三、结尾",
		"",
	}, "\n"), "\n")

	sections := ParseSections(lines)
	if len(sections) != 6 {
		t.Fatalf("section 数 = %d, want 6（# 一级标题不计入）", len(sections))
	}

	want := []struct {
		level int
		title string
	}{
		{2, "一、元信息"},
		{3, "1.1 点位列表"},
		{2, "二、汇总"},
		{3, "2.1 天文条件（近似算法）"},
		{3, "2026-08-12 夜（2026-08-12 22:00 → 次日 06:00）"},
		{2, "三、结尾"},
	}
	for i, w := range want {
		if sections[i].Level != w.level || sections[i].Title != w.title {
			t.Errorf("sections[%d] = {L%d %q}, want {L%d %q}",
				i, sections[i].Level, sections[i].Title, w.level, w.title)
		}
	}

	if sections[0].End != sections[2].Start {
		t.Errorf("sections[0].End = %d, want %d（下一个同级标题行）",
			sections[0].End, sections[2].Start)
	}

	if got := sections[len(sections)-1].End; got != len(lines) {
		t.Errorf("末节 End = %d, want %d", got, len(lines))
	}
}

func TestParseBlocks(t *testing.T) {
	md := []string{
		"普通段落与 **粗体** 一起。",
		"",
		"> 引用第一行",
		"> 引用第二行",
		"",
		"- 列表项 A",
		"- 列表项 B",
		"",
		"| 列1 | 列2 |",
		"| --- | --- |",
		"| a | b |",
		"",
		"---",
		"",
		"段落末。",
		"",
	}
	blocks := ParseBlocks(md)

	wantKinds := []BlockKind{BlockPara, BlockQuote, BlockList, BlockTable, BlockPara}
	if len(blocks) != len(wantKinds) {
		t.Fatalf("block 数 = %d, want %d（--- 水平线应被忽略）：%+v",
			len(blocks), len(wantKinds), blocks)
	}
	for i, k := range wantKinds {
		if blocks[i].Kind != k {
			t.Errorf("blocks[%d].Kind = %s, want %s", i, blocks[i].Kind, k)
		}
	}

	if blocks[0].Text != "普通段落与 粗体 一起。" {
		t.Errorf("段落未去掉 ** 标记：%q", blocks[0].Text)
	}
	if len(blocks[1].Lines) != 2 {
		t.Errorf("引用行数 = %d, want 2", len(blocks[1].Lines))
	}
	if len(blocks[2].Lines) != 2 {
		t.Errorf("列表项数 = %d, want 2", len(blocks[2].Lines))
	}
	if len(blocks[3].Header) != 2 || len(blocks[3].Rows) != 1 {
		t.Errorf("表格形状 = %d 列 x %d 行, want 2x1",
			len(blocks[3].Header), len(blocks[3].Rows))
	}
}

func TestParseBlocksTableWithoutSeparator(t *testing.T) {
	blocks := ParseBlocks([]string{"| a | b |", "| c | d |"})
	if len(blocks) != 1 || blocks[0].Kind != BlockTable {
		t.Fatalf("want 单个 table block, got %+v", blocks)
	}
	if len(blocks[0].Header) != 0 {
		t.Errorf("Header = %v, want 空（无分隔行）", blocks[0].Header)
	}
	if len(blocks[0].Rows) != 2 {
		t.Errorf("Rows 数 = %d, want 2", len(blocks[0].Rows))
	}
}

func TestParseBlocksTableRagged(t *testing.T) {
	blocks := ParseBlocks([]string{
		"| a | b | c |",
		"| --- | --- | --- |",
		"| 1 | 2 |",
		"|  |  |  |",
		"| 4 | 5 | 6 | 7 |",
	})
	tbl := blocks[0]
	if len(tbl.Header) != 3 {
		t.Fatalf("Header 列数 = %d, want 3", len(tbl.Header))
	}
	if len(tbl.Rows) != 2 {
		t.Fatalf("Rows 数 = %d, want 2（全空行应丢弃）：%+v", len(tbl.Rows), tbl.Rows)
	}
	for i, row := range tbl.Rows {
		if len(row) != 3 {
			t.Errorf("Rows[%d] 列数 = %d, want 3（应补齐/截断）", i, len(row))
		}
	}
	if tbl.Rows[0][2] != "" {
		t.Errorf("短行未补空串：%q", tbl.Rows[0][2])
	}
}

func TestCleanTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2.1 天文条件（近似算法…）", "天文条件"},
		{"核心窗口 23:00–05:00 通透小时数", "核心窗口"},
		{"一、元信息", "元信息"},
		{"1.1 点位列表", "点位列表"},
		{"普通标题", "普通标题"},
		{"**加粗标题**", "加粗标题"},

		{"2026-08-12 夜（2026-08-12 22:00 → 次日 06:00）", "2026-08-12 夜"},
		{"2026-08-12 夜", "2026-08-12 夜"},
	}
	for _, c := range cases {
		if got := CleanTitle(c.in); got != c.want {
			t.Errorf("CleanTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanTitleNeverEmpty(t *testing.T) {
	for _, in := range []string{"（全是括号说明）", "一、", "**"} {
		if got := CleanTitle(in); got == "" {
			t.Errorf("CleanTitle(%q) 返回空串，应回退到清洗前文本", in)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"点位列表", "sites"},
		{"天文条件", "astro"},
		{"核心窗口", "transparency"},
		{"低云海拔评估明细", "cloud_detail"},
		{"未映射小节", "未映射小节"},
		{"a b/c", "a_b_c"},
		{"!!!", "section"},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripEmoji(t *testing.T) {
	cases := []struct {
		in       string
		wantText string
		wantDot  bool
		wantCol  interface{}
	}{
		{"✅通透", "通透", true, ColorDotGreen},
		{"⚠️风险", "风险", true, ColorDotAmber},
		{"🔴不宜", "不宜", true, ColorDotRed},
		{"❓无数据", "无数据", true, ColorDotGray},
		{"无 emoji 文本", "无 emoji 文本", false, nil},
	}
	for _, c := range cases {
		text, dot := StripEmoji(c.in)
		if text != c.wantText {
			t.Errorf("StripEmoji(%q) text = %q, want %q", c.in, text, c.wantText)
		}
		if hasDot(dot) != c.wantDot {
			t.Errorf("StripEmoji(%q) hasDot = %v, want %v", c.in, hasDot(dot), c.wantDot)
		}
		if c.wantDot && dot != c.wantCol {
			t.Errorf("StripEmoji(%q) color = %v, want %v", c.in, dot, c.wantCol)
		}
	}
}

func TestStripEmojiFallbackLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"✅", "通透"},
		{"🔴", "不宜/放弃"},
		{"❓", "无数据"},
	}
	for _, c := range cases {
		if got, _ := StripEmoji(c.in); got != c.want {
			t.Errorf("StripEmoji(%q) = %q, want 兜底标签 %q", c.in, got, c.want)
		}
	}
}

func TestEmojiMapOrderStable(t *testing.T) {
	const mixed = "🔴✅⚠️"
	first, _ := StripEmoji(mixed)
	for i := 0; i < 50; i++ {
		got, dot := StripEmoji(mixed)
		if got != first {
			t.Fatalf("第 %d 次 StripEmoji 结果漂移：%q vs %q", i, got, first)
		}
		if dot != ColorDotRed {
			t.Fatalf("第 %d 次颜色漂移：%v, want 🔴 优先", i, dot)
		}
	}
}

func TestSplitTableRow(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"| a | b |", []string{"a", "b"}},
		{"a | b", []string{"a", "b"}},
		{"| **粗** | `码` |", []string{"粗", "码"}},
	}
	for _, c := range cases {
		got := SplitTableRow(c.in)
		if len(got) != len(c.want) {
			t.Errorf("SplitTableRow(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("SplitTableRow(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}
