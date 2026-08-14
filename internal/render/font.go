package render

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

var FontCandidates = []string{

	"/System/Library/Fonts/Hiragino Sans GB.ttc",
	"/System/Library/Fonts/Supplemental/Songti.ttc",
	"/Library/Fonts/Arial Unicode.ttf",

	`C:/Windows/Fonts/msyh.ttc`,
	`C:/Windows/Fonts/msyhbd.ttc`,
	`C:/Windows/Fonts/simhei.ttf`,
	`C:/Windows/Fonts/simsun.ttc`,

	"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
	"/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
	"/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/truetype/arphic/uming.ttc",
}

const probeRune rune = '山'

type Font struct {
	Face    font.Face
	Size    int
	Bold    bool
	metrics font.Metrics
	advance map[rune]fixed.Int26_6
}

func (f *Font) Ascent() fixed.Int26_6 { return f.metrics.Ascent }

func (f *Font) Advance(r rune) float64 {
	if a, ok := f.advance[r]; ok {
		return float64(a) / 64.0
	}
	a, ok := f.Face.GlyphAdvance(r)
	if !ok {

		a, _ = f.Face.GlyphAdvance('\uFFFD')
	}
	f.advance[r] = a
	return float64(a) / 64.0
}

func (f *Font) Measure(s string) float64 {
	if s == "" {
		return 0
	}
	return float64(font.MeasureString(f.Face, s)) / 64.0
}

func (f *Font) LineHeight() int {
	return int(float64(f.Size)*1.45) + 2
}

func (f *Font) InkHeight() int {
	h := f.metrics.Ascent + f.metrics.Descent
	return int((h + 63) / 64)
}

type fontKey struct {
	size int
	bold bool
}

type fontRegistry struct {
	mu       sync.Mutex
	path     string
	regular  *sfnt.Font
	bold     *sfnt.Font
	faceName string
	cache    map[fontKey]*Font
}

var registry = &fontRegistry{cache: make(map[fontKey]*Font)}

func ResolveFontPath(override string) (string, error) {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	override = strings.TrimSpace(override)
	if registry.path != "" && (override == "" || override == registry.path) {
		return registry.path, nil
	}

	var candidates []string
	if override != "" {
		candidates = []string{override}
	} else {
		candidates = FontCandidates
	}

	var failures []string
	for _, path := range candidates {
		if _, err := os.Stat(path); err != nil {
			failures = append(failures, fmt.Sprintf("%s → 文件不存在", path))
			continue
		}
		regular, bold, name, err := loadCollection(path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s → %v", path, err))
			continue
		}
		registry.path = path
		registry.regular = regular
		registry.bold = bold
		registry.faceName = name
		registry.cache = make(map[fontKey]*Font)
		return path, nil
	}

	hint := "请在 configs/config.json 的 douyin.font_path 中显式指定一个可用的中文 TrueType 字体"
	if override != "" {
		hint = "配置中 douyin.font_path 指定的字体不可用，请换一个中文 TrueType 字体（.ttf/.ttc）"
	}
	return "", fmt.Errorf("未找到可用的中文字体，%s。已尝试：\n  %s",
		hint, strings.Join(failures, "\n  "))
}

func FontPath() string {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.path
}

func FontFaceName() string {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.faceName
}

func ResetFontCache() {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.path = ""
	registry.regular = nil
	registry.bold = nil
	registry.faceName = ""
	registry.cache = make(map[fontKey]*Font)
}

func loadCollection(path string) (regular, bold *sfnt.Font, name string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("读取失败: %w", err)
	}
	coll, err := sfnt.ParseCollection(data)
	if err != nil {
		return nil, nil, "", fmt.Errorf("解析失败: %w", err)
	}

	var usable []*sfnt.Font
	var usableNames []string
	var lastErr error
	for i := 0; i < coll.NumFonts(); i++ {
		f, ferr := coll.Font(i)
		if ferr != nil {
			lastErr = ferr
			continue
		}
		var buf sfnt.Buffer
		idx, gerr := f.GlyphIndex(&buf, probeRune)
		if gerr != nil {
			lastErr = gerr
			continue
		}
		if idx == 0 {
			lastErr = fmt.Errorf("face[%d] 不含中文字形", i)
			continue
		}
		if _, gerr = f.LoadGlyph(&buf, idx, fixed.I(48), nil); gerr != nil {
			lastErr = gerr
			continue
		}
		full, nerr := f.Name(&buf, sfnt.NameIDFull)
		if nerr != nil {
			full = fmt.Sprintf("face[%d]", i)
		}
		usable = append(usable, f)
		usableNames = append(usableNames, full)
	}

	if len(usable) == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("集合内无可用 face")
		}
		return nil, nil, "", fmt.Errorf("无可用 face: %w", lastErr)
	}

	regular = usable[0]
	bold = usable[0]
	if len(usable) > 1 {

		bold = usable[1]
	}
	return regular, bold, usableNames[0], nil
}

func LoadFont(size int, bold bool) (*Font, error) {
	if size < 1 {
		size = 1
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()

	if registry.regular == nil {
		return nil, fmt.Errorf("字体尚未初始化，请先调用 ResolveFontPath")
	}
	key := fontKey{size: size, bold: bold}
	if f, ok := registry.cache[key]; ok {
		return f, nil
	}

	src := registry.regular
	if bold {
		src = registry.bold
	}
	face, err := opentype.NewFace(src, &opentype.FaceOptions{
		Size:    float64(size),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 %dpx 字体 face 失败: %w", size, err)
	}
	f := &Font{
		Face:    face,
		Size:    size,
		Bold:    bold,
		metrics: face.Metrics(),
		advance: make(map[rune]fixed.Int26_6, 128),
	}
	registry.cache[key] = f
	return f, nil
}

func mustLoadFont(size int, bold bool) *Font {
	f, err := LoadFont(size, bold)
	if err != nil {
		panic(err)
	}
	return f
}

func pyRound(v float64) int {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	fl := math.Floor(v)
	diff := v - fl
	switch {
	case diff > 0.5:
		return int(fl) + 1
	case diff < 0.5:
		return int(fl)
	default:
		if math.Mod(fl, 2) == 0 {
			return int(fl)
		}
		return int(fl) + 1
	}
}
