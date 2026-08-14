package render

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

const (
	CanvasW = 1080

	CanvasH = 1920

	Margin = 60

	ContentW = CanvasW - 2*Margin

	ContentTop = 180

	BottomLimit = 1915

	MeasureH = 12000
)

const (
	BaseTitle   = 66
	BaseSubhead = 44
	BaseBody    = 38
	BaseTable   = 32
	BaseSmall   = 30

	MinBody = 22
)

const (
	CardPadBase     = 10.0
	CardGapBase     = 8.0
	CardColGapBase  = 22.0
	CardLineFactor  = 1.19
	CardTitleDelta  = 4
	CardTitleFactor = 1.20

	CardPageMaxRows = 6
)

const (
	HeaderTop = 20

	HeaderTitleGapBase = 8.0

	HeaderSepGapBase = 12.0

	HeaderMetaGapBase = 24.0
)

const HardFloorScale = 0.4

var (
	ColorBG          = color.RGBA{0x0d, 0x11, 0x1c, 0xff}
	ColorAccent      = color.RGBA{0x36, 0xc5, 0xd6, 0xff}
	ColorSubheadFG   = color.RGBA{0x9f, 0xe8, 0xf0, 0xff}
	ColorTitleFG     = color.RGBA{0xff, 0xff, 0xff, 0xff}
	ColorBodyFG      = color.RGBA{0xe2, 0xe8, 0xf0, 0xff}
	ColorMutedFG     = color.RGBA{0xb9, 0xc2, 0xd0, 0xff}
	ColorQuoteBG     = color.RGBA{0x1e, 0x24, 0x30, 0xff}
	ColorCardBG      = color.RGBA{0x14, 0x1a, 0x26, 0xff}
	ColorCardBorder  = color.RGBA{0x30, 0x3c, 0x50, 0xff}
	ColorTableHeadBG = color.RGBA{0x14, 0x5e, 0x6a, 0xff}
	ColorTableHeadFG = color.RGBA{0xff, 0xff, 0xff, 0xff}
	ColorRowBGA      = color.RGBA{0x14, 0x1a, 0x26, 0xff}
	ColorRowBGB      = color.RGBA{0x1a, 0x21, 0x2f, 0xff}
	ColorGridLine    = color.RGBA{0x2e, 0x38, 0x4a, 0xff}
	ColorSepLine     = color.RGBA{0x2e, 0x38, 0x4a, 0xff}

	ColorDotRed   = color.RGBA{0xe0, 0x5a, 0x5a, 0xff}
	ColorDotGreen = color.RGBA{0x4e, 0xc9, 0x7a, 0xff}
	ColorDotAmber = color.RGBA{0xe8, 0xb1, 0x3a, 0xff}
	ColorDotGray  = color.RGBA{0x8a, 0x93, 0xa3, 0xff}
)

type Style struct {
	Scale float64

	FTitle     *Font
	FSubhead   *Font
	FBody      *Font
	FTable     *Font
	FTableHead *Font
	FCardTitle *Font
	FSmall     *Font

	GapBlock int
	PadCell  int
	PadCard  int
	GapCard  int
	PadQuote int
	BodySize int
}

func NewStyle(scale float64) *Style {
	s := scale
	return &Style{
		Scale:      s,
		FTitle:     mustLoadFont(pyRound(BaseTitle*s), true),
		FSubhead:   mustLoadFont(pyRound(BaseSubhead*s), true),
		FBody:      mustLoadFont(pyRound(BaseBody*s), false),
		FTable:     mustLoadFont(pyRound(BaseTable*s), false),
		FTableHead: mustLoadFont(pyRound(BaseTable*s), true),
		FCardTitle: mustLoadFont(pyRound((BaseTable+CardTitleDelta)*s), true),
		FSmall:     mustLoadFont(pyRound(BaseSmall*s), false),
		GapBlock:   maxInt(8, pyRound(26*s)),
		PadCell:    maxInt(6, pyRound(14*s)),
		PadCard:    maxInt(5, pyRound(CardPadBase*s)),
		GapCard:    maxInt(4, pyRound(CardGapBase*s)),
		PadQuote:   maxInt(8, pyRound(18*s)),
		BodySize:   pyRound(BaseBody * s),
	}
}

func (s *Style) DotRadius() int { return maxInt(4, pyRound(5*s.Scale)) }

func (s *Style) DotGap() int { return maxInt(6, pyRound(8*s.Scale)) }

func (s *Style) DotSlotWidth() int { return s.DotRadius()*2 + s.DotGap() }

func (s *Style) GapCardCol() int { return maxInt(12, pyRound(CardColGapBase*s.Scale)) }

func (s *Style) CardLineH() int {
	return compactLineH(s.FTable, CardLineFactor)
}

func (s *Style) CardTitleH() int {
	return compactLineH(s.FCardTitle, CardTitleFactor)
}

func compactLineH(f *Font, factor float64) int {
	floor := f.InkHeight() + 2
	return maxInt(floor, int(float64(f.Size)*factor)+2)
}

type Canvas struct {
	Img *image.RGBA
	W   int
	H   int
}

func NewCanvas(w, h int) *Canvas {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{ColorBG}, image.Point{}, draw.Src)
	return &Canvas{Img: img, W: w, H: h}
}

func NewMeasureCanvas(w, h int) *Canvas {
	return &Canvas{Img: nil, W: w, H: h}
}

func (c *Canvas) Drawing() bool { return c.Img != nil }

func (c *Canvas) FillRect(x0, y0, x1, y1 int, col color.RGBA) {
	if c.Img == nil {
		return
	}
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	r := image.Rect(x0, y0, x1+1, y1+1).Intersect(c.Img.Bounds())
	if r.Empty() {
		return
	}
	draw.Draw(c.Img, r, &image.Uniform{col}, image.Point{}, draw.Src)
}

func (c *Canvas) HLine(x0, x1, y, w int, col color.RGBA) {
	if c.Img == nil || w < 1 {
		return
	}
	c.FillRect(x0, y, x1, y+w-1, col)
}

func (c *Canvas) VLine(x, y0, y1, w int, col color.RGBA) {
	if c.Img == nil || w < 1 {
		return
	}
	c.FillRect(x, y0, x+w-1, y1, col)
}

func (c *Canvas) FillCircle(cx, cy, r int, col color.RGBA) {
	if c.Img == nil || r <= 0 {
		return
	}
	const ss = 4
	rf := float64(r)
	for py := cy - r - 1; py <= cy+r+1; py++ {
		for px := cx - r - 1; px <= cx+r+1; px++ {
			if !image.Pt(px, py).In(c.Img.Bounds()) {
				continue
			}
			hits := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					dx := float64(px) + (float64(sx)+0.5)/ss - float64(cx)
					dy := float64(py) + (float64(sy)+0.5)/ss - float64(cy)
					if dx*dx+dy*dy <= rf*rf {
						hits++
					}
				}
			}
			if hits == 0 {
				continue
			}
			c.blend(px, py, col, float64(hits)/float64(ss*ss))
		}
	}
}

func (c *Canvas) FillRoundRect(x0, y0, x1, y1, radius int, fill color.RGBA,
	outline color.RGBA, outlineW int) {
	if c.Img == nil {
		return
	}
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	w := x1 - x0 + 1
	h := y1 - y0 + 1
	if w <= 0 || h <= 0 {
		return
	}
	maxR := minInt(w, h) / 2
	if radius > maxR {
		radius = maxR
	}
	if radius < 0 {
		radius = 0
	}

	for py := y0; py <= y1; py++ {
		for px := x0; px <= x1; px++ {
			if !image.Pt(px, py).In(c.Img.Bounds()) {
				continue
			}
			cov := roundRectCoverage(float64(px), float64(py),
				float64(x0), float64(y0), float64(x1), float64(y1), float64(radius))
			if cov <= 0 {
				continue
			}
			c.blend(px, py, fill, cov)
		}
	}

	if outline.A == 0 || outlineW <= 0 {
		return
	}
	innerR := radius - outlineW
	if innerR < 0 {
		innerR = 0
	}
	ix0, iy0 := float64(x0+outlineW), float64(y0+outlineW)
	ix1, iy1 := float64(x1-outlineW), float64(y1-outlineW)
	for py := y0; py <= y1; py++ {
		for px := x0; px <= x1; px++ {
			if !image.Pt(px, py).In(c.Img.Bounds()) {
				continue
			}
			outer := roundRectCoverage(float64(px), float64(py),
				float64(x0), float64(y0), float64(x1), float64(y1), float64(radius))
			inner := 0.0
			if ix1 >= ix0 && iy1 >= iy0 {
				inner = roundRectCoverage(float64(px), float64(py),
					ix0, iy0, ix1, iy1, float64(innerR))
			}
			cov := outer - inner
			if cov <= 0 {
				continue
			}
			c.blend(px, py, outline, cov)
		}
	}
}

func roundRectCoverage(px, py, x0, y0, x1, y1, r float64) float64 {
	const ss = 3
	hits := 0
	for sy := 0; sy < ss; sy++ {
		for sx := 0; sx < ss; sx++ {
			x := px + (float64(sx)+0.5)/ss
			y := py + (float64(sy)+0.5)/ss
			if pointInRoundRect(x, y, x0, y0, x1, y1, r) {
				hits++
			}
		}
	}
	return float64(hits) / float64(ss*ss)
}

func pointInRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1+1 || y < y0 || y > y1+1 {
		return false
	}
	if r <= 0 {
		return true
	}

	lx, rx := x0+r, x1+1-r
	ty, by := y0+r, y1+1-r
	cx, cy := x, y
	if x < lx {
		cx = lx
	} else if x > rx {
		cx = rx
	}
	if y < ty {
		cy = ty
	} else if y > by {
		cy = by
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func (c *Canvas) blend(x, y int, col color.RGBA, a float64) {
	if a <= 0 {
		return
	}
	if a > 1 {
		a = 1
	}
	alpha := a * float64(col.A) / 255.0
	i := c.Img.PixOffset(x, y)
	pix := c.Img.Pix
	pix[i+0] = uint8(float64(col.R)*alpha + float64(pix[i+0])*(1-alpha) + 0.5)
	pix[i+1] = uint8(float64(col.G)*alpha + float64(pix[i+1])*(1-alpha) + 0.5)
	pix[i+2] = uint8(float64(col.B)*alpha + float64(pix[i+2])*(1-alpha) + 0.5)
	pix[i+3] = uint8(255*alpha + float64(pix[i+3])*(1-alpha) + 0.5)
}

func (c *Canvas) DrawText(x, y int, s string, f *Font, col color.RGBA) {
	if c.Img == nil || s == "" {
		return
	}
	d := &font.Drawer{
		Dst:  c.Img,
		Src:  &image.Uniform{col},
		Face: f.Face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y) + f.Ascent()},
	}
	d.DrawString(s)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
