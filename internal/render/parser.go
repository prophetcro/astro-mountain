package render

import (
	"image/color"
	"regexp"
	"strings"
)

const DefaultSections = "点位列表,天文条件,核心窗口,低云海拔评估明细"

var SlugMap = map[string]string{
	"点位列表":     "sites",
	"天文条件":     "astro",
	"核心窗口":     "transparency",
	"低云海拔评估明细": "cloud_detail",
}

const TableSplitThreshold = 6

var PagedKeywords = []string{"低云海拔评估明细"}

const NightTitleSep = " · "

const AstroSummaryPrefix = "[天文·近似]"

func IsAstroSummary(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), AstroSummaryPrefix)
}

type EmojiDot struct {
	Emoji string
	Color color.RGBA
	Label string
}

var EmojiMap = []EmojiDot{
	{"🔴", ColorDotRed, "不宜/放弃"},
	{"✅", ColorDotGreen, "通透"},
	{"⚠️", ColorDotAmber, "风险"},
	{"⚠", ColorDotAmber, "风险"},
	{"❓", ColorDotGray, "无数据"},
}

var (
	emojiRe = regexp.MustCompile(
		"[\U0001F300-\U0001FAFF" +
			"\U0001F000-\U0001F2FF" +
			"\u2600-\u26FF" +
			"\u2700-\u27BF" +
			"\u2B00-\u2BFF" +
			"\uFE0F\u200D\u20E3\u2049\u203C]")

	headingRe  = regexp.MustCompile(`^(#{2,6})\s+(.*)$`)
	tableSepRe = regexp.MustCompile(`^\|[\s:\-|]+\|?\s*$`)
	listRe     = regexp.MustCompile(`^\s*[-*]\s+(.*)$`)

	numPrefixRe  = regexp.MustCompile(`^\d{1,3}(?:\.\d+)*(?:[、.．]\s*|\s+)`)
	cnPrefixRe   = regexp.MustCompile(`^[一二三四五六七八九十百]+[、.．]\s*`)
	trailParenRe = regexp.MustCompile(`（[^）]*）\s*$`)

	trailTimeRe = regexp.MustCompile(`\s+\d{1,2}(?::\d{2})?[-–—]\d{1,2}(?::\d{2})?.*$`)

	nightSubheadingRe = regexp.MustCompile(`^###\s+(\d{4}-\d{2}-\d{2})\s*夜`)

	nonSlugRe = regexp.MustCompile(`[^0-9A-Za-z\x{4e00}-\x{9fff}]+`)
	spaceRe   = regexp.MustCompile(`\s+`)
)

type BlockKind string

const (
	BlockHeading BlockKind = "heading"
	BlockTable   BlockKind = "table"
	BlockQuote   BlockKind = "quote"
	BlockList    BlockKind = "list"
	BlockPara    BlockKind = "para"
)

type Block struct {
	Kind   BlockKind
	Level  int
	Text   string
	Lines  []string
	Header []string
	Rows   [][]string
}

type Section struct {
	Level int
	Title string
	Start int
	End   int
}

func CleanInline(text string) string {
	out := strings.ReplaceAll(text, "**", "")
	out = strings.ReplaceAll(out, "`", "")
	out = strings.ReplaceAll(out, `\|`, "|")
	out = strings.ReplaceAll(out, `\*`, "*")
	out = spaceRe.ReplaceAllString(out, " ")
	return strings.TrimSpace(out)
}

func CleanTitle(title string) string {
	base := CleanInline(title)
	out := cnPrefixRe.ReplaceAllString(base, "")
	out = numPrefixRe.ReplaceAllString(out, "")
	out = trailTimeRe.ReplaceAllString(out, "")
	out = trailParenRe.ReplaceAllString(out, "")
	out = strings.TrimSpace(out)
	if out == "" {

		return title
	}
	return out
}

func Slugify(keyword string) string {
	if s, ok := SlugMap[keyword]; ok {
		return s
	}
	slug := strings.Trim(nonSlugRe.ReplaceAllString(keyword, "_"), "_")
	if slug == "" {
		return "section"
	}
	return slug
}

func ParseSections(lines []string) []Section {
	var sections []Section
	for idx, raw := range lines {
		m := headingRe.FindStringSubmatch(strings.TrimRight(raw, " \t\r\n"))
		if m != nil {
			sections = append(sections, Section{
				Level: len(m[1]),
				Title: strings.TrimSpace(m[2]),
				Start: idx,
			})
		}
	}

	total := len(lines)
	for i := range sections {
		sections[i].End = total
		for _, nxt := range sections[i+1:] {
			if nxt.Level <= sections[i].Level {
				sections[i].End = nxt.Start
				break
			}
		}
	}
	return sections
}

func SplitTableRow(line string) []string {
	stripped := strings.TrimSpace(line)
	stripped = strings.TrimPrefix(stripped, "|")
	stripped = strings.TrimSuffix(stripped, "|")
	parts := strings.Split(stripped, "|")
	out := make([]string, 0, len(parts))
	for _, cell := range parts {
		out = append(out, CleanInline(cell))
	}
	return out
}

func ParseBlocks(lines []string) []Block {
	var blocks []Block
	i := 0
	n := len(lines)

	for i < n {
		raw := strings.TrimRight(lines[i], " \t\r\n")
		stripped := strings.TrimSpace(raw)

		if stripped == "" {
			i++
			continue
		}

		if m := headingRe.FindStringSubmatch(stripped); m != nil {
			blocks = append(blocks, Block{
				Kind:  BlockHeading,
				Level: len(m[1]),
				Text:  CleanTitle(m[2]),
			})
			i++
			continue
		}

		if strings.HasPrefix(stripped, "|") {
			var tableLines []string
			for i < n && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				tableLines = append(tableLines, strings.TrimSpace(lines[i]))
				i++
			}
			blocks = append(blocks, buildTableBlock(tableLines))
			continue
		}

		if strings.HasPrefix(stripped, ">") {
			var quoteLines []string
			for i < n && strings.HasPrefix(strings.TrimSpace(lines[i]), ">") {
				content := strings.TrimSpace(
					strings.TrimLeft(strings.TrimSpace(lines[i]), ">"))
				if content != "" {
					quoteLines = append(quoteLines, CleanInline(content))
				}
				i++
			}
			if len(quoteLines) > 0 {
				blocks = append(blocks, Block{Kind: BlockQuote, Lines: quoteLines})
			}
			continue
		}

		if listRe.MatchString(raw) {
			var items []string
			for i < n {
				m := listRe.FindStringSubmatch(strings.TrimRight(lines[i], " \t\r\n"))
				if m == nil {
					break
				}
				items = append(items, CleanInline(m[1]))
				i++
			}
			blocks = append(blocks, Block{Kind: BlockList, Lines: items})
			continue
		}

		if isRuleLine(stripped) {
			i++
			continue
		}

		var para []string
		for i < n {
			cur := strings.TrimRight(lines[i], " \t\r\n")
			curStripped := strings.TrimSpace(cur)
			if curStripped == "" ||
				strings.HasPrefix(curStripped, "|") ||
				strings.HasPrefix(curStripped, ">") ||
				headingRe.MatchString(curStripped) ||
				listRe.MatchString(cur) {
				break
			}
			para = append(para, CleanInline(curStripped))
			i++
		}
		joined := strings.TrimSpace(strings.Join(para, ""))
		if joined != "" {
			blocks = append(blocks, Block{Kind: BlockPara, Text: joined})
		}
	}

	return blocks
}

func isRuleLine(s string) bool {
	if len([]rune(s)) < 3 {
		return false
	}
	for _, r := range s {
		if r != '-' && r != '*' && r != '_' {
			return false
		}
	}
	return true
}

func buildTableBlock(tableLines []string) Block {
	var header []string
	var rows [][]string

	if len(tableLines) >= 2 && tableSepRe.MatchString(tableLines[1]) {
		header = SplitTableRow(tableLines[0])
		for _, ln := range tableLines[2:] {
			rows = append(rows, SplitTableRow(ln))
		}
	} else {
		for _, ln := range tableLines {
			rows = append(rows, SplitTableRow(ln))
		}
	}

	ncol := len(header)
	if ncol == 0 {
		for _, r := range rows {
			if len(r) > ncol {
				ncol = len(r)
			}
		}
	}
	if len(header) > 0 {
		header = padCells(header, ncol)
	}

	kept := make([][]string, 0, len(rows))
	for _, r := range rows {
		if !anyNonBlank(r) {
			continue
		}
		kept = append(kept, padCells(r, ncol))
	}
	return Block{Kind: BlockTable, Header: header, Rows: kept}
}

func padCells(cells []string, ncol int) []string {
	out := make([]string, ncol)
	for i := 0; i < ncol; i++ {
		if i < len(cells) {
			out[i] = cells[i]
		}
	}
	return out
}

func anyNonBlank(cells []string) bool {
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			return true
		}
	}
	return false
}
