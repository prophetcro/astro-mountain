package render

import (
	"io"
	"log/slog"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/config"
)

type Renderer struct {
	cfg config.DouyinConfig

	Logger *slog.Logger
}

var _ interface {
	Name() string
	Render(mdPath string, cfg config.Config, outDir string) ([]string, error)
} = (*Renderer)(nil)

func New(cfg config.DouyinConfig) *Renderer {
	return &Renderer{cfg: cfg}
}

func (r *Renderer) Name() string { return "douyin" }

func (r *Renderer) Render(mdPath string, cfg config.Config, outDir string) ([]string, error) {
	return RenderDouyin(mdPath, r.options(cfg.Douyin, outDir))
}

func (r *Renderer) options(d config.DouyinConfig, outDir string) Options {
	return Options{
		OutDir:   outDir,
		Sections: joinSections(pickSections(d.Sections, r.cfg.Sections)),
		FontPath: pickString(d.FontPath, r.cfg.FontPath),
		Logger:   r.logger(),
	}
}

func (r *Renderer) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func pickSections(primary, fallback []string) []string {
	if len(nonEmpty(primary)) > 0 {
		return primary
	}
	return fallback
}

func pickString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func joinSections(sections []string) string {
	kept := nonEmpty(sections)
	if len(kept) == 0 {
		return ""
	}
	return strings.Join(kept, ",")
}

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
