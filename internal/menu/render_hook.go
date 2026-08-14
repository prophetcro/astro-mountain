package menu

import (
	"io"
	"log/slog"

	"github.com/prophetcro/astro-mountain/internal/render"
)

func defaultRenderReport(mdPath, outDir, sections, fontPath string) ([]string, error) {
	return render.RenderDouyin(mdPath, render.Options{
		OutDir:   outDir,
		Sections: sections,
		FontPath: fontPath,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func defaultFontProbe(override string) (string, error) {
	return render.ResolveFontPath(override)
}
