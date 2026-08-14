package meteoblue

import (
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func fptr(v float64) *float64 { return &v }

func testSite() model.Site {
	return model.Site{Name: "星辰山", Lat: 28.2656, Lon: 119.3788, Alt: 1000.0, Timezone: "Asia/Shanghai"}
}

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("加载时区失败：%v", err)
	}
	return loc
}

// 构造一个含「白天 / 夜间 / 跨零点凌晨」三时刻的响应，夜间窗口与 targetNights
// 过滤应只保留后两个。字段名严格对照 openapi.yml（timeformat=iso8601 → RFC3339）。
// 用 Data3h（免费档默认 basic-3h+clouds-3h 的真实返回块）验证 DataBlockOf 选型。
func threeTimeResponse() *MetoResponse {
	return &MetoResponse{Data3h: &DataBlock{
		Time:              []string{"2026-08-12T10:00:00+08:00", "2026-08-12T22:00:00+08:00", "2026-08-13T03:00:00+08:00"},
		Temperature:       []*float64{fptr(20), fptr(15), fptr(13)},
		RelativeHumidity:  []*float64{fptr(60), fptr(80), fptr(90)},
		Precipitation:     []*float64{fptr(0), fptr(0), fptr(0)},
		PrecipProbability: []*float64{fptr(0), fptr(0), fptr(0)},
		WindSpeed10m:      []*float64{fptr(3), fptr(4), fptr(5)},
		Visibility:        []*float64{fptr(12000), fptr(10000), fptr(8000)},
		TotalCloudCover:   []*float64{fptr(20), fptr(30), fptr(40)},
		LowClouds:         []*float64{fptr(10), fptr(20), fptr(30)},
		MidClouds:         []*float64{fptr(10), fptr(10), fptr(10)},
		HighClouds:        []*float64{fptr(5), fptr(5), fptr(5)},
		FogProbability:    []*float64{fptr(5), fptr(5), fptr(5)},
		Pictocode:         nil,
	}}
}

func TestEvaluateResponseFiltersNightWindowAndTargetNights(t *testing.T) {
	loc := shanghai(t)
	cfg := config.Default()
	resp := threeTimeResponse()
	targetNights := map[string]bool{"2026-08-12": true}
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, loc)
	end := time.Date(2026, 8, 14, 0, 0, 0, 0, loc)

	rows := EvaluateResponse(testSite(), resp, start, end, targetNights, &cfg, loc)
	if len(rows) != 2 {
		t.Fatalf("期望 2 行（夜间 + 跨零点凌晨），得到 %d 行：%v", len(rows), rows)
	}
	for _, r := range rows {
		if !r.HasData {
			t.Errorf("Meteoblue 行 HasData 应为 true，得到 false（%s）", r.TimeISO)
		}
		// 两行都应归入 2026-08-12 夜（凌晨 03:00 回拨 12h 仍归前一夜）。
		if r.Night != "2026-08-12" {
			t.Errorf("Night 期望 2026-08-12，得到 %q（%s）", r.Night, r.TimeISO)
		}
	}
}

func TestEvaluateResponsePrecipitationVetoesToBad(t *testing.T) {
	loc := shanghai(t)
	cfg := config.Default()
	resp := threeTimeResponse()
	// 把 22:00 这一刻设为有降水。
	resp.Data3h.Precipitation[1] = fptr(2.5)
	resp.Data3h.PrecipProbability[1] = fptr(80)
	targetNights := map[string]bool{"2026-08-12": true}
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, loc)
	end := time.Date(2026, 8, 14, 0, 0, 0, 0, loc)

	rows := EvaluateResponse(testSite(), resp, start, end, targetNights, &cfg, loc)
	if len(rows) != 2 {
		t.Fatalf("期望 2 行，得到 %d", len(rows))
	}
	// 找到 22:00 行。
	var rain *model.HourRow
	for i := range rows {
		if rows[i].TimeISO == "2026-08-12T22:00" {
			rain = &rows[i]
		}
	}
	if rain == nil {
		t.Fatal("没找到 22:00 这一行的评估")
	}
	if rain.Rating != model.RATING_BAD {
		t.Errorf("有降水的时刻应判 %q，得到 %q（note=%s）", model.RATING_BAD, rain.Rating, rain.Note)
	}
	if !strings.Contains(rain.Note, "降水") {
		t.Errorf("BAD 行的 note 应提到降水，得到 %q", rain.Note)
	}
	// Relation 填固定占位说明，而非留空（ safety.AuditRows 要求有数据行必须
	// 带有效关系分类，同时向用户诚实说明 C 轨不做云海几何）。
	if !rain.Relation.Valid || rain.Relation.V != relationMeteoblueNoGeometry {
		t.Errorf("Relation 应为 %q，得到 %v", relationMeteoblueNoGeometry, rain.Relation)
	}
	if rain.CloudLowSource.V != "meteoblue" {
		t.Errorf("CloudLowSource 应标 meteoblue，得到 %q", rain.CloudLowSource.V)
	}
}

func TestEvaluateResponseCleanNightStaysOK(t *testing.T) {
	loc := shanghai(t)
	cfg := config.Default()
	resp := threeTimeResponse()
	targetNights := map[string]bool{"2026-08-12": true}
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, loc)
	end := time.Date(2026, 8, 14, 0, 0, 0, 0, loc)

	rows := EvaluateResponse(testSite(), resp, start, end, targetNights, &cfg, loc)
	// 全部低/中/高云都偏低、无降水、能见度良好 → OK。
	for i := range rows {
		if rows[i].Rating != model.RATING_OK {
			t.Errorf("%s 应判 %q，得到 %q（note=%s）",
				rows[i].TimeISO, model.RATING_OK, rows[i].Rating, rows[i].Note)
		}
	}
}

func TestEvaluateResponseFogVetoesToBad(t *testing.T) {
	loc := shanghai(t)
	cfg := config.Default()
	resp := threeTimeResponse()
	// 把 22:00 能见度压到雾阈值以下（默认 FogVisibilityM 通常 1000m）。
	resp.Data3h.Visibility[1] = fptr(300)
	targetNights := map[string]bool{"2026-08-12": true}
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, loc)
	end := time.Date(2026, 8, 14, 0, 0, 0, 0, loc)

	rows := EvaluateResponse(testSite(), resp, start, end, targetNights, &cfg, loc)
	var fog *model.HourRow
	for i := range rows {
		if rows[i].TimeISO == "2026-08-12T22:00" {
			fog = &rows[i]
		}
	}
	if fog == nil {
		t.Fatal("没找到 22:00 这一行")
	}
	if fog.Rating != model.RATING_BAD {
		t.Errorf("能见度 < 雾阈值的时刻应判 %q，得到 %q（note=%s）",
			model.RATING_BAD, fog.Rating, fog.Note)
	}
}

func TestEvaluateResponseFogProbabilityVetoesToBad(t *testing.T) {
	loc := shanghai(t)
	cfg := config.Default()
	resp := threeTimeResponse()
	// 把 22:00 的雾概率拉到硬否决阈值以上（≥50%）。
	resp.Data3h.FogProbability[1] = fptr(70)
	targetNights := map[string]bool{"2026-08-12": true}
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, loc)
	end := time.Date(2026, 8, 14, 0, 0, 0, 0, loc)

	rows := EvaluateResponse(testSite(), resp, start, end, targetNights, &cfg, loc)
	var fog *model.HourRow
	for i := range rows {
		if rows[i].TimeISO == "2026-08-12T22:00" {
			fog = &rows[i]
		}
	}
	if fog == nil {
		t.Fatal("没找到 22:00 这一行")
	}
	if fog.Rating != model.RATING_BAD {
		t.Errorf("雾概率 ≥50%% 的时刻应判 %q，得到 %q（note=%s）",
			model.RATING_BAD, fog.Rating, fog.Note)
	}
	if !strings.Contains(fog.Note, "雾概率") {
		t.Errorf("BAD 行的 note 应提到雾概率，得到 %q", fog.Note)
	}
}

func TestDataBlockOfSelectsCloudsBlock(t *testing.T) {
	// 免费档：只返回 data_3h，且含低云字段 → 必须选它。
	r1 := &MetoResponse{Data3h: &DataBlock{
		Time:      []string{"2026-08-12T22:00:00+08:00"},
		LowClouds: []*float64{fptr(20)},
	}}
	if got := r1.DataBlockOf(); got == nil || got != r1.Data3h {
		t.Fatalf("应选中 data_3h 块，得到 %v", got)
	}

	// 混合：同时有 data_1h（无云）与 data_3h（有云）→ 选含云量的 data_3h。
	r2 := &MetoResponse{
		Data1h: &DataBlock{Time: []string{"2026-08-12T22:00:00+08:00"},
			Temperature: []*float64{fptr(15)}},
		Data3h: &DataBlock{Time: []string{"2026-08-12T21:00:00+08:00"},
			LowClouds: []*float64{fptr(20)}},
	}
	if got := r2.DataBlockOf(); got == nil || got != r2.Data3h {
		t.Fatalf("混合情况下应选中含云量的 data_3h，得到 %v", got)
	}

	// 全空：返回 nil，评估层应优雅跳过（不崩）。
	var r3 *MetoResponse
	if got := r3.DataBlockOf(); got != nil {
		t.Fatalf("空响应应返回 nil，得到 %v", got)
	}
}

func TestParseMetoTimeRFC3339(t *testing.T) {
	loc := shanghai(t)
	tm, err := parseMetoTime("2026-08-12T22:00:00+08:00", loc)
	if err != nil {
		t.Fatalf("RFC3339 解析失败：%v", err)
	}
	if tm.Hour() != 22 || tm.Minute() != 0 {
		t.Errorf("期望 22:00，得到 %02d:%02d", tm.Hour(), tm.Minute())
	}
	// 偏移应正确反映 +08:00（用 Zone()，不能用 Sub——Sub 比较的是同一时刻，恒为 0）。
	if _, off := tm.Zone(); off != 8*3600 {
		t.Errorf("期望 UTC+8 偏移，得到 %ds", off)
	}
	// Meteoblue 真实返回：不带秒的 RFC3339（2026-08-12T22:00+08:00）。
	// 此前只认带秒格式，导致所有时刻解析失败、静默 0 行——这是线上实测暴露的 bug。
	tmNoSec, err := parseMetoTime("2026-08-12T22:00+08:00", loc)
	if err != nil {
		t.Fatalf("无秒 RFC3339 解析失败（真实 Meteoblue 格式）：%v", err)
	}
	if tmNoSec.Hour() != 22 {
		t.Errorf("无秒 RFC3339 应解析为 22:00，得到 %02d:%02d", tmNoSec.Hour(), tmNoSec.Minute())
	}
	// 裸本地时间回退到 loc。
	tm2, err := parseMetoTime("2026-08-12T22:00", loc)
	if err != nil {
		t.Fatalf("裸时间回退解析失败：%v", err)
	}
	if tm2.Hour() != 22 {
		t.Errorf("裸时间应解析为 22:00，得到 %02d:%02d", tm2.Hour(), tm2.Minute())
	}
}
