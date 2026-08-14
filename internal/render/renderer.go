package render

import (
	"fmt"
	"image"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

type Options struct {
	OutDir string

	Sections string

	FontPath string

	Stem string

	Logger *slog.Logger
}

func (o Options) sectionKeywords() []string {
	raw := strings.TrimSpace(o.Sections)
	if raw == "" {
		raw = DefaultSections
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if k := strings.TrimSpace(part); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func (o Options) logger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

func measureSection(title string, blocks []Block, scale float64) (*Style, int) {
	style := NewStyle(scale)
	r := NewSectionRenderer(style, NewMeasureCanvas(CanvasW, MeasureH))
	r.DrawHeader(title)
	r.DrawBlocks(blocks)
	return style, r.Y
}

func renderSection(title string, blocks []Block, scale float64, canvasH int) (*image.RGBA, int) {
	style := NewStyle(scale)
	cv := NewCanvas(CanvasW, canvasH)
	r := NewSectionRenderer(style, cv)

	_, totalH := measureSection(title, blocks, scale)
	if totalH < BottomLimit {
		r.YOffset = (BottomLimit - totalH) / 2
	}

	r.DrawHeader(title)
	r.DrawBlocks(blocks)
	return cv.Img, r.Y
}

func RenderToCanvas(title string, blocks []Block) (*image.RGBA, float64) {
	scale := 1.0
	chosen := 1.0
	for {
		_, bottom := measureSection(title, blocks, scale)
		if bottom <= BottomLimit {
			chosen = scale
			break
		}
		next := scale * 0.9
		if next < HardFloorScale {
			chosen = next
			break
		}
		scale = next
	}
	img, _ := renderSection(title, blocks, chosen, CanvasH)
	return img, chosen
}

type Result struct {
	Path   string
	Title  string
	Scale  float64
	Width  int
	Height int
}

func RenderDouyin(mdPath string, opts Options) (outputs []string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			outputs = nil
			err = fmt.Errorf("抖音出图内部异常（已捕获，不影响主流程）: %v", rec)
		}
	}()

	results, err := RenderDouyinDetailed(mdPath, opts)
	if err != nil {
		return nil, err
	}
	outputs = make([]string, 0, len(results))
	for _, r := range results {
		outputs = append(outputs, r.Path)
	}
	return outputs, nil
}

func RenderDouyinDetailed(mdPath string, opts Options) ([]Result, error) {
	log := opts.logger()

	absMD, err := filepath.Abs(mdPath)
	if err != nil {
		return nil, fmt.Errorf("报告路径无法解析：%s: %w", mdPath, err)
	}
	data, err := os.ReadFile(absMD)
	if err != nil {
		return nil, fmt.Errorf("报告文件读取失败：%s: %w", absMD, err)
	}

	fontPath, err := ResolveFontPath(opts.FontPath)
	if err != nil {
		return nil, err
	}

	lines := splitLines(string(data))
	sections := ParseSections(lines)
	if len(sections) == 0 {
		return nil, fmt.Errorf("报告中未解析到任何标题：%s", absMD)
	}

	outDir := opts.OutDir
	if strings.TrimSpace(outDir) == "" {
		outDir = filepath.Join(filepath.Dir(absMD), "douyin")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("输出目录创建失败：%s: %w", outDir, err)
	}

	stem := opts.Stem
	if strings.TrimSpace(stem) == "" {
		base := filepath.Base(absMD)
		stem = strings.TrimSuffix(base, filepath.Ext(base))
	}

	log.Info("抖音出图开始", "report", absMD, "font", fontPath,
		"face", FontFaceName(), "out_dir", outDir)

	var results []Result
	for _, keyword := range opts.sectionKeywords() {
		parent := PickSection(sections, keyword)
		targets := FindSubsectionsForKeyword(sections, keyword)
		if parent == nil || len(targets) == 0 {
			log.Warn("跳过：未找到匹配小节", "keyword", keyword)
			continue
		}

		for _, sec := range targets {
			blocks := ParseBlocks(lines[sec.Start+1 : sec.End])
			if len(blocks) == 0 {
				log.Warn("跳过：小节正文为空", "section", sec.Title)
				continue
			}

			title, slug := ComposeOutputMeta(*parent, sec, keyword)
			pages := PaginateSection(title, blocks, keyword)

			if pages == nil {

				res, err := writePage(outDir, fmt.Sprintf("%s_%s.png", stem, slug), title, blocks)
				if err != nil {
					return nil, err
				}
				results = append(results, res)
				log.Info("已生成", "path", res.Path, "size",
					fmt.Sprintf("%dx%d", res.Width, res.Height),
					"scale", res.Scale, "title", res.Title)
				continue
			}

			for i, page := range pages {
				name := fmt.Sprintf("%s_%s_%d.png", stem, slug, i+1)
				res, err := writePage(outDir, name, page.Title, page.Blocks)
				if err != nil {
					return nil, err
				}
				results = append(results, res)
				log.Info("已生成", "path", res.Path, "size",
					fmt.Sprintf("%dx%d", res.Width, res.Height),
					"scale", res.Scale, "title", res.Title)
			}
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("未生成任何图片（报告中没有匹配到任何目标小节）：%s", absMD)
	}
	log.Info("抖音出图完成", "count", len(results))
	return results, nil
}

func writePage(outDir, filename, title string, blocks []Block) (Result, error) {
	img, scale := RenderToCanvas(title, blocks)
	outPath := filepath.Join(outDir, filename)
	fh, err := os.Create(outPath)
	if err != nil {
		return Result{}, fmt.Errorf("图片写入失败：%s: %w", outPath, err)
	}
	if err := png.Encode(fh, img); err != nil {
		fh.Close()
		return Result{}, fmt.Errorf("PNG 编码失败：%s: %w", outPath, err)
	}
	if err := fh.Close(); err != nil {
		return Result{}, fmt.Errorf("图片关闭失败：%s: %w", outPath, err)
	}
	b := img.Bounds()
	return Result{
		Path:   outPath,
		Title:  title,
		Scale:  scale,
		Width:  b.Dx(),
		Height: b.Dy(),
	}, nil
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")

	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
