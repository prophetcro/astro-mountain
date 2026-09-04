package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// SunriseExportFilename 推导日出模式 JSON 导出文件名，与 Markdown 报告同名同目录、
// 仅扩展名不同（.json），避免同一目录互相覆盖。多日模式与 Markdown 一致使用「首_末」区间命名。
func SunriseExportFilename(results []SunriseSiteResult, suffix string) string {
	base := sunriseReportFilename(results) // 形如 astro_report_sunrise-2026-09-06.md
	return strings.TrimSuffix(base, ".md") + suffix
}

// sunriseJSONPayload 日出模式 JSON 的结构：运行元信息 + 派生阈值 + 各站点逐日出聚合结果。
// 与流星雨模式的 jsonPayload 平级，但 rows 换成 sunrise 模式的 []SunriseSiteResult。
type sunriseJSONPayload struct {
	Meta    model.ReportMeta      `json:"meta"`
	Config  configExport          `json:"config"`
	Results []SunriseSiteResult   `json:"results"`
}

// BuildSunriseJSON 序列化日出模式聚合结果为 JSON 字节。
// time.Time 字段（含 Episodes 内嵌时段）按 RFC3339 输出；无数据时的零值时间亦如实保留。
func BuildSunriseJSON(results []SunriseSiteResult, meta model.ReportMeta, cfg config.Config) ([]byte, error) {
	payload := sunriseJSONPayload{
		Meta:    meta,
		Config:  newConfigExport(cfg),
		Results: results,
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		return nil, fmt.Errorf("序列化日出 JSON 失败：%w", err)
	}
	return buf.Bytes(), nil
}

// ExportSunriseJSON 把日出模式聚合结果写出为 JSON 文件。
func ExportSunriseJSON(path string, results []SunriseSiteResult, meta model.ReportMeta, cfg config.Config) error {
	data, err := BuildSunriseJSON(results, meta, cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("写入 JSON 文件 %s 失败：%w", path, err)
	}
	return nil
}
