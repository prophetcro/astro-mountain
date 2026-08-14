package render

import (
	"fmt"
	"strings"
	"testing"
)

func TestPickSection(t *testing.T) {
	sections := []Section{
		{Level: 2, Title: "一、元信息", Start: 0, End: 10},
		{Level: 3, Title: "1.1 点位列表", Start: 4, End: 10},
		{Level: 2, Title: "四、低云海拔评估明细", Start: 10, End: 30},
	}
	if got := PickSection(sections, "点位列表"); got == nil || got.Start != 4 {
		t.Errorf("PickSection(点位列表) = %+v, want Start=4", got)
	}
	if got := PickSection(sections, "低云海拔评估明细"); got == nil || got.Start != 10 {
		t.Errorf("PickSection(低云海拔评估明细) = %+v, want Start=10", got)
	}
	if got := PickSection(sections, "不存在的小节"); got != nil {
		t.Errorf("PickSection(不存在) = %+v, want nil", got)
	}
}

func TestHeadingLine(t *testing.T) {
	cases := []struct {
		sec  Section
		want string
	}{
		{Section{Level: 2, Title: "元信息"}, "## 元信息"},
		{Section{Level: 3, Title: "2026-08-12 夜"}, "### 2026-08-12 夜"},
		{Section{Level: 4, Title: "深层"}, "#### 深层"},
	}
	for _, c := range cases {
		if got := HeadingLine(c.sec); got != c.want {
			t.Errorf("HeadingLine(%+v) = %q, want %q", c.sec, got, c.want)
		}
	}
}

func TestNightDateOf(t *testing.T) {
	cases := []struct {
		sec  Section
		want string
	}{
		{Section{Level: 3, Title: "2026-08-12 夜（2026-08-12 22:00 → 次日 06:00）"}, "2026-08-12"},
		{Section{Level: 3, Title: "2026-08-12 夜"}, "2026-08-12"},
		{Section{Level: 3, Title: "2026-08-12夜"}, "2026-08-12"},
		{Section{Level: 3, Title: "天文条件"}, ""},
		{Section{Level: 2, Title: "2026-08-12 夜"}, ""},
		{Section{Level: 4, Title: "2026-08-12 夜"}, ""},
	}
	for _, c := range cases {
		if got := NightDateOf(c.sec); got != c.want {
			t.Errorf("NightDateOf(L%d %q) = %q, want %q",
				c.sec.Level, c.sec.Title, got, c.want)
		}
	}
}

func TestFindSubsectionsForKeyword(t *testing.T) {
	sections := []Section{
		{Level: 2, Title: "四、低云海拔评估明细", Start: 0, End: 40},
		{Level: 3, Title: "2026-08-12 夜（…）", Start: 10, End: 25},
		{Level: 3, Title: "2026-08-13 夜（…）", Start: 25, End: 40},
		{Level: 2, Title: "五、导出字段说明", Start: 40, End: 50},
	}

	got := FindSubsectionsForKeyword(sections, "低云海拔评估明细")
	if len(got) != 2 {
		t.Fatalf("按夜子节数 = %d, want 2：%+v", len(got), got)
	}
	if NightDateOf(got[0]) != "2026-08-12" || NightDateOf(got[1]) != "2026-08-13" {
		t.Errorf("子节顺序错误：%q, %q", got[0].Title, got[1].Title)
	}

	got = FindSubsectionsForKeyword(sections, "导出字段说明")
	if len(got) != 1 || got[0].Title != "五、导出字段说明" {
		t.Errorf("无子节场景 = %+v, want [大节本身]", got)
	}

	if got = FindSubsectionsForKeyword(sections, "不存在"); len(got) != 0 {
		t.Errorf("未命中 = %+v, want 空", got)
	}
}

func TestComposeOutputMeta(t *testing.T) {
	cases := []struct {
		name      string
		parent    Section
		sec       Section
		keyword   string
		wantTitle string
		wantSlug  string
	}{
		{
			name:      "按夜子节",
			parent:    Section{Level: 2, Title: "四、低云海拔评估明细"},
			sec:       Section{Level: 3, Title: "2026-08-12 夜（2026-08-12 22:00 → 次日 06:00）"},
			keyword:   "低云海拔评估明细",
			wantTitle: "低云海拔评估明细 · 2026-08-12 夜",
			wantSlug:  "cloud_detail_2026-08-12",
		},
		{
			name:      "普通小节",
			parent:    Section{Level: 3, Title: "2.2 核心窗口 23:00–05:00 通透小时数"},
			sec:       Section{Level: 3, Title: "2.2 核心窗口 23:00–05:00 通透小时数"},
			keyword:   "核心窗口",
			wantTitle: "核心窗口",
			wantSlug:  "transparency",
		},
		{
			name:      "点位列表",
			parent:    Section{Level: 3, Title: "1.1 点位列表"},
			sec:       Section{Level: 3, Title: "1.1 点位列表"},
			keyword:   "点位列表",
			wantTitle: "点位列表",
			wantSlug:  "sites",
		},
		{
			name:      "天文条件",
			parent:    Section{Level: 3, Title: "2.1 天文条件（近似算法，取各点位经纬度均值）"},
			sec:       Section{Level: 3, Title: "2.1 天文条件（近似算法，取各点位经纬度均值）"},
			keyword:   "天文条件",
			wantTitle: "天文条件",
			wantSlug:  "astro",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			title, slug := ComposeOutputMeta(c.parent, c.sec, c.keyword)
			if title != c.wantTitle {
				t.Errorf("title = %q, want %q", title, c.wantTitle)
			}
			if slug != c.wantSlug {
				t.Errorf("slug = %q, want %q", slug, c.wantSlug)
			}
		})
	}
}

func TestPageTitleSuffixSurvivesCleanTitle(t *testing.T) {
	pageTitle := fmt.Sprintf("%s（%d/%d）", "低云海拔评估明细 · 2026-08-12 夜", 1, 3)
	if !strings.HasSuffix(pageTitle, "（1/3）") {
		t.Fatalf("页标题后缀错误：%q", pageTitle)
	}

	if cleaned := CleanTitle(pageTitle); strings.Contains(cleaned, "1/3") {
		t.Errorf("CleanTitle 竟保留了页码 %q —— 若实现改动请同步复核 PaginateSection", cleaned)
	}
}

func TestFindSplittableTableNoCandidate(t *testing.T) {

	blocks := []Block{{
		Kind:   BlockTable,
		Header: []string{"a", "b"},
		Rows:   make([][]string, TableSplitThreshold),
	}}
	if got := FindSplittableTable("标题", blocks, "低云海拔评估明细"); got != -1 {
		t.Errorf("FindSplittableTable = %d, want -1（行数未超阈值）", got)
	}
}

func TestPaginateSectionByKeyword(t *testing.T) {
	requireFont(t)

	const nRows = 24
	rows := make([][]string, nRows)
	for i := range rows {
		rows[i] = []string{
			fmt.Sprintf("点位%d", i), "1650", "6/6", "23:00-05:00",
			"-800", "-300", "云海在脚下", "✅通透", "整夜可拍", "云底在机位下方约 800m",
		}
	}
	blocks := []Block{{
		Kind:   BlockTable,
		Header: []string{"点位", "海拔m", "有效h", "窗口", "云底", "云顶", "位置", "状态", "结论", "备注"},
		Rows:   rows,
	}}

	pages := PaginateSection("低云海拔评估明细 · 2026-08-12 夜", blocks, "低云海拔评估明细")
	if len(pages) < 2 {
		t.Fatalf("页数 = %d, want >= 2（%d 行 10 列高表应分页）", len(pages), nRows)
	}

	total := 0
	for i, p := range pages {
		wantSuffix := fmt.Sprintf("（%d/%d）", i+1, len(pages))
		if !strings.HasSuffix(p.Title, wantSuffix) {
			t.Errorf("page[%d].Title = %q, want 后缀 %q", i, p.Title, wantSuffix)
		}

		var tbl *Block
		for j := range p.Blocks {
			if p.Blocks[j].Kind == BlockTable {
				tbl = &p.Blocks[j]
				break
			}
		}
		if tbl == nil {
			t.Fatalf("page[%d] 没有表格块", i)
		}
		if len(tbl.Header) != 10 {
			t.Errorf("page[%d] 表头列数 = %d, want 10（每页应重复表头）", i, len(tbl.Header))
		}

		if !FitsFullScale(p.Title, p.Blocks) {
			t.Errorf("page[%d] 在 scale=1.0 下放不下——贪心装箱应保证每页都放得下", i)
		}
		total += len(tbl.Rows)
	}

	if total != nRows {
		t.Errorf("分页后总行数 = %d, want %d（不能丢行）", total, nRows)
	}
}

func TestPaginateSectionSinglePageReturnsNil(t *testing.T) {
	requireFont(t)

	rows := make([][]string, 5)
	for i := range rows {
		rows[i] = []string{fmt.Sprintf("r%d", i), "x"}
	}
	pages := PaginateSection("低云海拔评估明细", []Block{{
		Kind: BlockTable, Header: []string{"a", "b"}, Rows: rows,
	}}, "低云海拔评估明细")
	if len(pages) != 0 {
		t.Errorf("页数 = %d, want 0（nil：整表可单页放下就不分页）", len(pages))
	}
}

func TestFindSplittableTableRespectsFitsFullScale(t *testing.T) {
	requireFont(t)

	rows := make([][]string, 8)
	for i := range rows {
		rows[i] = []string{fmt.Sprintf("点位%d", i), "1650"}
	}
	blocks := []Block{{Kind: BlockTable, Header: []string{"点位", "海拔"}, Rows: rows}}

	if FitsFullScale("点位列表", blocks) {
		if got := FindSplittableTable("点位列表", blocks, "点位列表"); got != -1 {
			t.Errorf("FindSplittableTable = %d, want -1（能放下就不该拆）", got)
		}
	} else {
		t.Log("该字体下 8 行表放不下 scale=1.0，跳过「不拆」断言")
	}
}
