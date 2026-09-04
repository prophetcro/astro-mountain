package report

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func TestBuildSunriseJSONStructure(t *testing.T) {
	fixed := time.Date(2026, 9, 6, 5, 30, 0, 0, time.UTC)
	results := []SunriseSiteResult{
		{
			Site:          "佘山",
			SunriseTime:   fixed,
			ArriveBy:      fixed.Add(-90 * time.Minute),
			SunriseDate:   "2026-09-06",
			CloudSeaHours: 2,
			CloudSeaForm:  "脚下型",
			DawnGlow:      "大烧",
			FogPotential:  "强",
			Confidence:    "高",
			Rating:        "✅ 建议前往",
			Episodes: []CloudSeaEpisode{
				{Start: fixed.Add(-3 * time.Hour), End: fixed.Add(-1 * time.Hour),
					TopMSL: 120.0, TopAGL: 20.0, Submerged: false, Kind: "脚下型",
					PeakThickness: 80.0, HoursCount: 2, MissingHours: 0},
			},
		},
	}
	meta := model.ReportMeta{Mode: "sunrise", Start: "2026-09-06", End: "2026-09-06"}

	data, err := BuildSunriseJSON(results, meta, config.Default())
	if err != nil {
		t.Fatalf("BuildSunriseJSON 失败：%v", err)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("产出不是合法 JSON：%v\n%s", err, string(data))
	}
	for _, key := range []string{"meta", "config", "results"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("JSON 缺少顶层字段 %q", key)
		}
	}

	var got []SunriseSiteResult
	if err := json.Unmarshal(doc["results"], &got); err != nil {
		t.Fatalf("results 反序列化失败：%v", err)
	}
	if len(got) != 1 || got[0].Site != "佘山" {
		t.Fatalf("results 内容错误：%#v", got)
	}
	if got[0].Episodes[0].Kind != "脚下型" {
		t.Errorf("嵌套 Episodes 未正确序列化：%#v", got[0].Episodes)
	}

	// 文件名推导：与 Markdown 同名、扩展名不同。
	if name := SunriseExportFilename(results, ".json"); name != "astro_report_sunrise-2026-09-06.json" {
		t.Errorf("SunriseExportFilename 错误：%q", name)
	}
}

func TestBuildSunriseJSONMultiDay(t *testing.T) {
	results := []SunriseSiteResult{
		{Site: "A", SunriseDate: "2026-09-06"},
		{Site: "A", SunriseDate: "2026-09-07"},
	}
	if name := SunriseExportFilename(results, ".json"); !strings.Contains(name, "2026-09-06_2026-09-07") {
		t.Errorf("多日命名错误：%q", name)
	}
}

func TestExportSunriseJSONWritesFile(t *testing.T) {
	dir := t.TempDir()
	results := []SunriseSiteResult{{Site: "B", SunriseDate: "2026-09-06"}}
	meta := model.ReportMeta{Mode: "sunrise"}
	path := dir + "/astro_report_sunrise-2026-09-06.json"

	if err := ExportSunriseJSON(path, results, meta, config.Default()); err != nil {
		t.Fatalf("ExportSunriseJSON 失败：%v", err)
	}
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), `"Site":`) {
		t.Fatalf("写入内容校验失败：err=%v data=%s", err, string(data))
	}
}
