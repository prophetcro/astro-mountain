package render

import (
	"fmt"
	"image/color"
	"strings"
)

var noDot = color.RGBA{}

func hasDot(c color.RGBA) bool { return c.A != 0 }

func StripEmoji(text string) (string, color.RGBA) {
	dot := noDot
	label := ""
	for _, e := range EmojiMap {
		if strings.Contains(text, e.Emoji) {
			dot = e.Color
			label = e.Label
			break
		}
	}

	cleaned := emojiRe.ReplaceAllString(text, "")
	cleaned = strings.TrimSpace(spaceRe.ReplaceAllString(cleaned, " "))
	if cleaned == "" && label != "" {
		cleaned = label
	}
	return cleaned, dot
}

func WrapText(text string, f *Font, maxWidth float64) []string {
	if maxWidth <= 0 {
		return []string{text}
	}
	if text == "" {
		return []string{""}
	}

	var lines []string
	var cur []rune
	curW := 0.0

	for _, ch := range text {
		if ch == '\n' {
			lines = append(lines, string(cur))
			cur = cur[:0]
			curW = 0
			continue
		}
		adv := f.Advance(ch)
		if len(cur) == 0 || curW+adv <= maxWidth {
			cur = append(cur, ch)
			curW += adv
			continue
		}

		if idx := lastSpaceIndex(cur); idx >= 0 {
			head, tail := string(cur[:idx]), string(cur[idx+1:])
			if strings.TrimSpace(head) != "" {
				lines = append(lines, head)
				cur = append([]rune{}, []rune(tail+string(ch))...)
				curW = f.Measure(string(cur))
				continue
			}
		}
		lines = append(lines, string(cur))
		cur = append(cur[:0], ch)
		curW = adv
	}
	lines = append(lines, string(cur))
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func lastSpaceIndex(cur []rune) int {
	for i := len(cur) - 1; i >= 0; i-- {
		if cur[i] == ' ' {
			return i
		}
	}
	return -1
}

func WrappedHeight(text string, f *Font, maxWidth float64) int {
	return len(WrapText(text, f, maxWidth)) * f.LineHeight()
}

type SectionRenderer struct {
	St      *Style
	Cv      *Canvas
	Y       int
	YOffset int
	err     error
}

func NewSectionRenderer(style *Style, canvas *Canvas) *SectionRenderer {
	return &SectionRenderer{St: style, Cv: canvas}
}

func (r *SectionRenderer) drawWrapped(text string, f *Font, x, y int,
	maxW float64, col color.RGBA, dot color.RGBA) int {
	return r.drawWrappedLH(text, f, x, y, maxW, col, dot, f.LineHeight())
}

func (r *SectionRenderer) drawWrappedLH(text string, f *Font, x, y int,
	maxW float64, col color.RGBA, dot color.RGBA, lineH int) int {
	textX := x
	if hasDot(dot) {
		radius := r.St.DotRadius()
		cy := y + lineH/2

		r.Cv.FillCircle(x+radius, cy, radius, dot)
		textX = x + 2*radius + r.St.DotGap()
		maxW -= float64(textX - x)
	}

	for _, line := range WrapText(text, f, maxW) {
		r.Cv.DrawText(textX, y, line, f, col)
		y += lineH
	}
	return y
}

func (r *SectionRenderer) wrappedLineCount(text string, f *Font, maxW float64,
	dot color.RGBA) int {
	if hasDot(dot) {
		maxW -= float64(r.St.DotSlotWidth())
	}
	return len(WrapText(text, f, maxW))
}

func splitHeaderTitle(title string) (main, meta string) {
	idx := strings.Index(title, NightTitleSep)
	if idx < 0 {
		return title, ""
	}
	main = strings.TrimSpace(title[:idx])
	meta = strings.TrimSpace(title[idx+len(NightTitleSep):])
	if main == "" || meta == "" {
		return title, ""
	}
	return main, meta
}

func (r *SectionRenderer) DrawHeader(title string) {
	st := r.St
	barW := maxInt(6, pyRound(8*st.Scale))
	textX := Margin + barW + maxInt(14, pyRound(22*st.Scale))
	lineH := st.FTitle.LineHeight()
	avail := float64(ContentW - (textX - Margin))
	top := HeaderTop + r.YOffset

	main, meta := splitHeaderTitle(title)
	metaW := 0.0
	metaGap := 0
	if meta != "" {
		metaW = st.FSmall.Measure(meta)
		metaGap = maxInt(12, pyRound(HeaderMetaGapBase*st.Scale))

		if metaW+float64(metaGap) >= avail {
			main, meta = title, ""
			metaW, metaGap = 0, 0
		}
	}

	titleW := avail - metaW - float64(metaGap)
	lines := WrapText(main, st.FTitle, titleW)
	blockH := lineH * len(lines)

	r.Cv.FillRect(Margin, top, Margin+barW, top+blockH, ColorAccent)
	y := top
	for _, line := range lines {
		r.Cv.DrawText(textX, y, line, st.FTitle, ColorTitleFG)
		y += lineH
	}

	if meta != "" {

		metaY := top + (st.FTitle.Ascent() - st.FSmall.Ascent()).Round()
		metaX := CanvasW - Margin - int(metaW+0.5)
		r.Cv.DrawText(metaX, metaY, meta, st.FSmall, ColorMutedFG)
	}

	sepY := top + blockH + maxInt(6, pyRound(HeaderTitleGapBase*st.Scale))
	r.Cv.HLine(Margin, CanvasW-Margin, sepY, 2, ColorSepLine)
	r.Y = sepY + maxInt(10, pyRound(HeaderSepGapBase*st.Scale))
}

func (r *SectionRenderer) DrawBlocks(blocks []Block) {
	for i := range blocks {
		blk := blocks[i]
		switch blk.Kind {
		case BlockHeading:
			r.drawHeading(blk)
		case BlockTable:
			r.DrawTable(blk)
		case BlockQuote:
			r.DrawQuote(blk)
		case BlockList:
			r.DrawList(blk)
		default:
			r.drawPara(blk)
		}
		r.Y += r.St.GapBlock
	}
}

func (r *SectionRenderer) drawHeading(blk Block) {
	text, dot := StripEmoji(blk.Text)
	r.Y = r.drawWrapped(text, r.St.FSubhead, Margin, r.Y,
		float64(ContentW), ColorSubheadFG, dot)
}

func (r *SectionRenderer) drawPara(blk Block) {
	text, dot := StripEmoji(blk.Text)
	f := r.St.FBody
	if IsAstroSummary(text) {

		f = r.St.FSmall
	}
	r.Y = r.drawWrapped(text, f, Margin, r.Y, float64(ContentW), ColorBodyFG, dot)
}

func (r *SectionRenderer) DrawList(blk Block) {
	st := r.St
	indent := maxInt(16, pyRound(26*st.Scale))
	bulletW := st.FBody.Measure("• ")
	off := maxInt(indent, int(bulletW))
	for _, item := range blk.Lines {
		text, dot := StripEmoji(item)
		r.Cv.DrawText(Margin, r.Y, "•", st.FBody, ColorAccent)
		r.Y = r.drawWrapped(text, st.FBody, Margin+off, r.Y,
			float64(ContentW-off), ColorBodyFG, dot)
	}
}

func (r *SectionRenderer) DrawQuote(blk Block) {
	st := r.St
	const barW = 4
	innerX := Margin + barW + st.PadQuote
	innerW := ContentW - barW - st.PadQuote*2
	lineH := st.FSmall.LineHeight()

	total := 0
	for _, ln := range blk.Lines {
		text, _ := StripEmoji(ln)
		total += len(WrapText(text, st.FSmall, float64(innerW))) * lineH
	}
	boxH := total + st.PadQuote*2

	r.Cv.FillRoundRect(Margin, r.Y, CanvasW-Margin, r.Y+boxH,
		maxInt(6, pyRound(12*st.Scale)), ColorQuoteBG, noDot, 0)
	r.Cv.FillRect(Margin, r.Y, Margin+barW, r.Y+boxH, ColorAccent)

	ty := r.Y + st.PadQuote
	for _, ln := range blk.Lines {
		text, dot := StripEmoji(ln)
		ty = r.drawWrapped(text, st.FSmall, innerX, ty, float64(innerW), ColorMutedFG, dot)
	}
	r.Y += boxH
}

func (r *SectionRenderer) DrawTable(blk Block) {
	ncol := len(blk.Header)
	if ncol == 0 && len(blk.Rows) > 0 {
		ncol = len(blk.Rows[0])
	}
	if ncol == 0 || len(blk.Rows) == 0 {
		return
	}
	if ncol <= 6 && ncol*150 <= ContentW {
		r.DrawGridTable(blk, ncol)
	} else {
		r.DrawCardTable(blk, ncol)
	}
}

func (r *SectionRenderer) columnWidths(blk Block, ncol int) []int {
	st := r.St
	pad2 := float64(st.PadCell * 2)
	natural := make([]float64, ncol)
	for c := 0; c < ncol; c++ {
		w := 0.0
		if len(blk.Header) > 0 {
			w = st.FTableHead.Measure(blk.Header[c])
		}
		for _, row := range blk.Rows {
			text, _ := StripEmoji(row[c])
			w = maxFloat(w, st.FTable.Measure(text))
		}
		natural[c] = w + pad2
	}

	minW := float64(st.FTable.Size*3) + pad2
	total := 0.0
	for c := range natural {
		natural[c] = maxFloat(natural[c], minW)
		total += natural[c]
	}

	widths := make([]int, ncol)
	sum := 0
	for c := range natural {
		widths[c] = int(natural[c] / total * ContentW)
		sum += widths[c]
	}
	widths[ncol-1] += ContentW - sum
	return widths
}

func (r *SectionRenderer) DrawGridTable(blk Block, ncol int) {
	st := r.St
	widths := r.columnWidths(blk, ncol)

	if len(blk.Header) > 0 {
		headH := 0
		for c := 0; c < ncol; c++ {
			h := WrappedHeight(blk.Header[c], st.FTableHead,
				float64(widths[c]-st.PadCell*2))
			if h > headH {
				headH = h
			}
		}
		headH += st.PadCell * 2
		r.Cv.FillRect(Margin, r.Y, CanvasW-Margin, r.Y+headH, ColorTableHeadBG)
		x := Margin
		for c := 0; c < ncol; c++ {
			r.drawWrapped(blk.Header[c], st.FTableHead, x+st.PadCell, r.Y+st.PadCell,
				float64(widths[c]-st.PadCell*2), ColorTableHeadFG, noDot)
			x += widths[c]
		}
		r.Y += headH
	}

	for rIdx, row := range blk.Rows {
		texts := make([]string, ncol)
		dots := make([]color.RGBA, ncol)
		rowH := 0
		for c := 0; c < ncol; c++ {
			texts[c], dots[c] = StripEmoji(row[c])
			avail := float64(widths[c] - st.PadCell*2)
			if hasDot(dots[c]) {
				avail -= float64(st.DotSlotWidth())
			}
			if h := WrappedHeight(texts[c], st.FTable, avail); h > rowH {
				rowH = h
			}
		}
		rowH += st.PadCell * 2

		bg := ColorRowBGA
		if rIdx%2 != 0 {
			bg = ColorRowBGB
		}
		r.Cv.FillRect(Margin, r.Y, CanvasW-Margin, r.Y+rowH, bg)

		x := Margin
		for c := 0; c < ncol; c++ {
			r.drawWrapped(texts[c], st.FTable, x+st.PadCell, r.Y+st.PadCell,
				float64(widths[c]-st.PadCell*2), ColorBodyFG, dots[c])
			x += widths[c]
			if c < ncol-1 {
				r.Cv.VLine(x, r.Y, r.Y+rowH, 1, ColorGridLine)
			}
		}
		r.Cv.HLine(Margin, CanvasW-Margin, r.Y+rowH, 1, ColorGridLine)
		r.Y += rowH
	}
}

type cardField struct {
	Label string
	Value string
	Dot   color.RGBA
}

type cardRow struct {
	Fields []cardField
	Widths []int
	Lines  int
}

func (r *SectionRenderer) cardFieldWidth(f cardField) int {
	w := r.St.FTable.Measure(f.Label) + r.St.FTable.Measure(f.Value)
	if hasDot(f.Dot) {
		w += float64(r.St.DotSlotWidth())
	}
	return int(w + 0.5)
}

func (r *SectionRenderer) packCardFields(fields []cardField, innerW, colGap int) []cardRow {
	st := r.St
	var rows []cardRow
	var cur cardRow
	curW := 0

	flush := func() {
		if len(cur.Fields) == 0 {
			return
		}
		cur.Lines = 1
		for i, f := range cur.Fields {
			avail := float64(cur.Widths[i]) - st.FTable.Measure(f.Label)
			if n := r.wrappedLineCount(f.Value, st.FTable, avail, f.Dot); n > cur.Lines {
				cur.Lines = n
			}
		}
		rows = append(rows, cur)
		cur = cardRow{}
		curW = 0
	}

	for _, f := range fields {
		nat := r.cardFieldWidth(f)
		if len(cur.Fields) > 0 && curW+colGap+nat > innerW {
			flush()
		}
		if len(cur.Fields) > 0 {
			curW += colGap
		}
		w := minInt(nat, innerW)
		cur.Fields = append(cur.Fields, f)
		cur.Widths = append(cur.Widths, w)
		curW += w
	}
	flush()
	return rows
}

type cardLayout struct {
	title      string
	dot        color.RGBA
	rows       []cardRow
	titleLines int
	headH      int
	bodyH      int
	cardH      int
}

func (r *SectionRenderer) DrawCardTable(blk Block, ncol int) {
	st := r.St

	labels := blk.Header
	if len(labels) == 0 {
		labels = make([]string, ncol)
		for i := 0; i < ncol; i++ {
			labels[i] = fmt.Sprintf("字段%d", i+1)
		}
	}

	innerW := ContentW - st.PadCard*2
	colGap := st.GapCardCol()
	lineH := st.CardLineH()
	titleH := st.CardTitleH()

	titleGap := maxInt(2, pyRound(4*st.Scale))
	radius := maxInt(6, pyRound(10*st.Scale))

	layouts := make([]cardLayout, 0, len(blk.Rows))
	for _, row := range blk.Rows {
		cardTitle, cardDot := StripEmoji(row[0])
		fields := make([]cardField, 0, ncol-1)
		for c := 1; c < ncol; c++ {
			value, dot := StripEmoji(row[c])
			fields = append(fields, cardField{Label: labels[c] + "：", Value: value, Dot: dot})
		}

		cardRows := r.packCardFields(fields, innerW, colGap)
		titleLines := r.wrappedLineCount(cardTitle, st.FCardTitle, float64(innerW), cardDot)
		headH := titleLines*titleH + titleGap

		bodyH := 0
		for _, cr := range cardRows {
			bodyH += cr.Lines * lineH
		}
		cardH := st.PadCard*2 + headH + bodyH

		layouts = append(layouts, cardLayout{
			title: cardTitle, dot: cardDot, rows: cardRows,
			titleLines: titleLines, headH: headH, bodyH: bodyH, cardH: cardH,
		})
	}

	for _, cl := range layouts {
		cardH := cl.cardH
		r.Cv.FillRoundRect(Margin, r.Y, CanvasW-Margin, r.Y+cardH,
			radius, ColorCardBG, ColorCardBorder, 1)

		cy := r.Y + st.PadCard
		r.drawWrappedLH(cl.title, st.FCardTitle, Margin+st.PadCard, cy,
			float64(innerW), ColorSubheadFG, cl.dot, titleH)
		cy += cl.headH

		for _, cr := range cl.rows {
			cx := Margin + st.PadCard
			for i, f := range cr.Fields {
				r.Cv.DrawText(cx, cy, f.Label, st.FTable, ColorMutedFG)
				vx := cx + int(st.FTable.Measure(f.Label))
				avail := float64(cr.Widths[i]) - st.FTable.Measure(f.Label)
				r.drawWrappedLH(f.Value, st.FTable, vx, cy, avail, ColorBodyFG, f.Dot, lineH)
				cx += cr.Widths[i] + colGap
			}
			cy += cr.Lines * lineH
		}

		r.Y += cardH + st.GapCard
	}
	if len(layouts) > 0 {
		r.Y -= st.GapCard
	}
}
