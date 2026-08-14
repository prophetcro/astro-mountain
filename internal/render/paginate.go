package render

import (
	"fmt"
	"strings"
)

type Page struct {
	Title  string
	Blocks []Block
}

func PickSection(sections []Section, keyword string) *Section {
	for i := range sections {
		if strings.Contains(sections[i].Title, keyword) {
			return &sections[i]
		}
	}
	return nil
}

func HeadingLine(sec Section) string {
	return strings.Repeat("#", sec.Level) + " " + sec.Title
}

func NightDateOf(sec Section) string {
	m := nightSubheadingRe.FindStringSubmatch(HeadingLine(sec))
	if m == nil {
		return ""
	}
	return m[1]
}

func FindSubsectionsForKeyword(sections []Section, keyword string) []Section {
	parent := PickSection(sections, keyword)
	if parent == nil {
		return nil
	}
	var nights []Section
	for _, sub := range sections {
		if parent.Start < sub.Start && sub.Start < parent.End && NightDateOf(sub) != "" {
			nights = append(nights, sub)
		}
	}
	if len(nights) > 0 {
		return nights
	}
	return []Section{*parent}
}

func ComposeOutputMeta(parent Section, sec Section, keyword string) (title, slug string) {
	baseSlug := Slugify(keyword)
	nightDate := NightDateOf(sec)
	if nightDate == "" {
		return CleanTitle(sec.Title), baseSlug
	}
	display := CleanTitle(parent.Title) + NightTitleSep + CleanTitle(sec.Title)
	return display, fmt.Sprintf("%s_%s", baseSlug, nightDate)
}

func FitsFullScale(title string, blocks []Block) bool {
	_, bottom := measureSection(title, blocks, 1.0)
	return bottom <= BottomLimit
}

func FindSplittableTable(title string, blocks []Block, keyword string) int {
	first := -1
	for idx, blk := range blocks {
		if blk.Kind == BlockTable && len(blk.Rows) > TableSplitThreshold {
			first = idx
			break
		}
	}
	if first < 0 {
		return -1
	}
	for _, kw := range PagedKeywords {
		if kw == keyword {
			return first
		}
	}
	if FitsFullScale(title, blocks) {
		return -1
	}
	return first
}

func AttachContext(title string, table Block, before, after []Block) []Block {
	var options [][]Block
	if len(before) > 0 && len(after) > 0 {
		options = append(options, concatBlocks(before, []Block{table}, after))
	}
	if len(before) > 0 {
		options = append(options, concatBlocks(before, []Block{table}, nil))
	}
	if len(after) > 0 {
		options = append(options, concatBlocks(nil, []Block{table}, after))
	}
	options = append(options, []Block{table})

	for _, candidate := range options {
		if FitsFullScale(title, candidate) {
			return candidate
		}
	}
	return []Block{table}
}

func concatBlocks(parts ...[]Block) []Block {
	total := 0
	for _, p := range parts {
		total += len(p)
	}
	out := make([]Block, 0, total)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func PaginateSection(title string, blocks []Block, keyword string) []Page {
	idx := FindSplittableTable(title, blocks, keyword)
	if idx < 0 {
		return nil
	}

	table := blocks[idx]
	before := append([]Block(nil), blocks[:idx]...)
	after := append([]Block(nil), blocks[idx+1:]...)

	maxRows := 1
	for k := len(table.Rows); k >= 1; k-- {
		cand := concatBlocks(before, []Block{{
			Kind:   BlockTable,
			Header: append([]string(nil), table.Header...),
			Rows:   table.Rows[:k],
		}}, after)
		if FitsFullScale(title, cand) {
			maxRows = k
			break
		}
	}
	if maxRows > CardPageMaxRows {
		maxRows = CardPageMaxRows
	}
	var chunks [][][]string
	for i := 0; i < len(table.Rows); i += maxRows {
		end := i + maxRows
		if end > len(table.Rows) {
			end = len(table.Rows)
		}
		chunks = append(chunks, table.Rows[i:end])
	}
	total := len(chunks)
	if total < 2 {
		return nil
	}

	pages := make([]Page, 0, total)
	for i, chunk := range chunks {
		pageNo := i + 1
		pageTitle := fmt.Sprintf("%s（%d/%d）", title, pageNo, total)
		pageTable := Block{
			Kind:   BlockTable,
			Header: append([]string(nil), table.Header...),
			Rows:   chunk,
		}

		pageAfter := []Block(nil)
		if pageNo == total {
			pageAfter = after
		}
		pages = append(pages, Page{
			Title:  pageTitle,
			Blocks: AttachContext(pageTitle, pageTable, before, pageAfter),
		})
	}
	return pages
}
