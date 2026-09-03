package core

import (
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
	"github.com/prophetcro/astro-mountain/internal/profile"
	"github.com/prophetcro/astro-mountain/internal/report"
)

// cloudsea_merge_test.go 锁死云海时段**合并**的行为，覆盖 2026-09 修掉的三个缺陷：
//  1. 廓线缺测被当成「无云海」——8h 连续云海被切成 2 段、HoursCount 少计 1。
//     现口径：缺测不切段、不计入 HoursCount，改记 MissingHours 由渲染层如实标注。
//  2. 合并间隔硬编码 1h —— 3h 分辨率下 9h 真实覆盖被切成 3 段各 1h，末段还少算 2h。
//     现口径：间隔与结束时刻都由 resp.Times 的真实采样间隔推导。
//  3. 结束时刻 off-by-one —— N 小时时段少算 1h。
//
// 这些用例原本是取证探针（断言「缺陷存在」，以 FAIL 暴露问题）；
// 缺陷修复后统一翻转为回归断言（断言「正确行为」），防止再退化。

const mergeNight = "2026-09-15"

var mergeSite = Site{Name: "牛草山", Lat: 31.047, Lon: 116.259, Alt: 1442}

// mergeLevelVars 是 fixture 里实际填了数值的气压层变量名。
var mergeLevelVars = []string{
	"cloud_cover_900hPa", "cloud_cover_850hPa", "cloud_cover_800hPa",
	"relative_humidity_900hPa", "relative_humidity_850hPa", "relative_humidity_800hPa",
}

// blankLevelProfileAt 把第 idx 小时的全部气压层云量/湿度置为缺测，
// 模拟「该小时廓线整体缺测」。
func blankLevelProfileAt(resp *api.Response, idx int) {
	for _, name := range mergeLevelVars {
		s, ok := resp.Series[name]
		if !ok || idx < 0 || idx >= len(s) {
			continue
		}
		s[idx] = model.Missing()
	}
}

// dumpEpisodes 打印时段合并结果并返回累计 HoursCount。
func dumpEpisodes(t *testing.T, tag string, eps []report.CloudSeaEpisode) int {
	t.Helper()
	total := 0
	for _, e := range eps {
		total += e.HoursCount
	}
	t.Logf("[%s] 段数=%d  累计HoursCount=%d", tag, len(eps), total)
	for i, e := range eps {
		t.Logf("[%s]   段%d: %s → %s  HoursCount=%d  MissingHours=%d  报告跨度=%.1fh",
			tag, i, e.Start.Format("15:04"), e.End.Format("15:04"),
			e.HoursCount, e.MissingHours, e.End.Sub(e.Start).Hours())
	}
	if len(eps) == 0 {
		t.Logf("[%s]   （无时段）", tag)
	}
	return total
}

// makeCloudSeaResp3h 构造与 makeCloudSeaResp 同形态、但时间轴为 3 小时间隔的响应
// （23:00 / 02:00 / 05:00），用于检验合并逻辑对非 1h 分辨率的防御。
func makeCloudSeaResp3h(t *testing.T) *api.Response {
	t.Helper()

	resp := &api.Response{
		Latitude:         31.047,
		Longitude:        116.259,
		Elevation:        1442,
		UTCOffsetSeconds: 28800,
		Timezone:         "Asia/Shanghai",
	}
	start := time.Date(2026, 9, 15, 23, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		resp.Times = append(resp.Times, start.Add(time.Duration(i)*3*time.Hour))
	}

	sur := map[string][]float64{
		"temperature_2m":       {18, 17, 16},
		"dew_point_2m":         {14, 14, 14},
		"relative_humidity_2m": {85, 92, 96},
		"cloud_cover_low":      {60, 60, 60},
		"cloud_cover_mid":      {20, 20, 20},
		"cloud_cover_high":     {10, 10, 10},
		"wind_speed_10m":       {1, 1, 1},
		"visibility":           {20000, 20000, 20000},
		"weather_code":         {0, 0, 0},
	}
	all := map[string][]float64{
		"cloud_cover_900hPa":         {60, 60, 60},
		"cloud_cover_850hPa":         {0, 0, 0},
		"cloud_cover_800hPa":         {0, 0, 0},
		"geopotential_height_900hPa": {983, 983, 983},
		"geopotential_height_850hPa": {1477, 1477, 1477},
		"geopotential_height_800hPa": {1996, 1996, 1996},
		"relative_humidity_900hPa":   {90, 90, 90},
		"relative_humidity_850hPa":   {50, 50, 50},
		"relative_humidity_800hPa":   {40, 40, 40},
	}
	for k, v := range sur {
		all[k] = v
	}
	resp.Series = make(map[string][]model.OptFloat, len(all))
	for name, vs := range all {
		optVals := make([]model.OptFloat, len(vs))
		for j, v := range vs {
			optVals[j] = model.Num(v)
		}
		resp.Series[name] = optVals
	}
	resp.HourlyVars = api.BuildHourlyVars(true)
	return resp
}

// ---------------------------------------------------------------------------
// 基线
// ---------------------------------------------------------------------------

// TestCloudSeaMerge_Baseline 基线：夜窗内 8 小时连续有云海。
// 同时锁死 off-by-one 修复：末小时 06:00 的时段结束时刻必须是 07:00，跨度 == HoursCount。
func TestCloudSeaMerge_Baseline(t *testing.T) {
	cfg := config.Default()
	resp := makeCloudSeaResp(t)

	eps := CollectCloudSeaEpisodesForNight(mergeSite, resp, mergeNight, cfg)
	total := dumpEpisodes(t, "基线·8h连续有云海", eps)

	if len(eps) != 1 || total != 8 {
		t.Fatalf("基线不符合预期：期望 1 段 / 8 时次，实际 %d 段 / %d 时次", len(eps), total)
	}
	if span := eps[0].End.Sub(eps[0].Start).Hours(); span != 8 {
		t.Errorf("时段跨度 %.0fh 与 HoursCount=%d 不一致（off-by-one 回归）",
			span, eps[0].HoursCount)
	}
	if eps[0].MissingHours != 0 {
		t.Errorf("基线无缺测，MissingHours 应为 0，实际 %d", eps[0].MissingHours)
	}
}

// ---------------------------------------------------------------------------
// 缺测容错：不切段、不计入时长、如实标注
// ---------------------------------------------------------------------------

// TestCloudSeaMerge_MissingHourKeepsEpisode 中间 1 小时廓线整体缺测时，
// 连续云海必须保持 1 段；缺测不计入 HoursCount，但记入 MissingHours。
//
// 这是 2026-09 修掉的缺陷：修复前缺测与「无云海」被同等对待，
// 8h 连续云海被切成 2 段（23:00→02:00 / 03:00→07:00），且输出与「真无云」逐字节相同。
func TestCloudSeaMerge_MissingHourKeepsEpisode(t *testing.T) {
	cfg := config.Default()
	resp := makeCloudSeaResp(t)
	const missIdx = 3 // 当地时间 02:00

	// 自证 1：置空前该小时廓线可用（有数据）。
	before := profile.BuildProfile(resp.LevelValues(missIdx), cfg.Thresh)
	if !ProfileUsable(before) {
		t.Fatalf("fixture 构造失败：idx=%d 置空前就不可用（levels=%d）", missIdx, len(before))
	}

	// 自证 2：置空后廓线不可用，即该小时状态未知。
	blankLevelProfileAt(resp, missIdx)
	after := profile.BuildProfile(resp.LevelValues(missIdx), cfg.Thresh)
	t.Logf("[缺测] idx=%d(02:00) 置空后 层数 %d→%d，ProfileUsable %v→%v",
		missIdx, len(before), len(after), ProfileUsable(before), ProfileUsable(after))
	if ProfileUsable(after) {
		t.Fatalf("fixture 构造失败：idx=%d 置空后仍可用（levels=%d）", missIdx, len(after))
	}

	eps := CollectCloudSeaEpisodesForNight(mergeSite, resp, mergeNight, cfg)
	total := dumpEpisodes(t, "缺测·02:00廓线整体缺测", eps)

	// 关键回归点：不再被切成 2 段。
	if len(eps) != 1 {
		t.Fatalf("缺测把连续云海切成了 %d 段，期望 1 段（缺测不等于云海散了）", len(eps))
	}
	// 缺测不计入时长：8 个时次里 1 个缺测，故为 7。
	if total != 7 {
		t.Errorf("HoursCount=%d，期望 7（缺测时次不计入时长，改记 MissingHours）", total)
	}
	// 缺测如实标注，不能静默吞掉。
	if eps[0].MissingHours != 1 {
		t.Errorf("MissingHours=%d，期望 1（缺测必须如实标注）", eps[0].MissingHours)
	}
	// End 仍以最后一个【有云海】的时次为准：06:00 + 1h = 07:00。
	if want := time.Date(2026, 9, 16, 7, 0, 0, 0, time.UTC); !eps[0].End.Equal(want) {
		t.Errorf("End=%s，期望 %s（应以最后一个有云海时次为准，尾部缺测不延长时间）",
			eps[0].End.Format("15:04"), want.Format("15:04"))
	}
}

// TestCloudSeaMerge_GenuineNoCloudSplits 中间 1 小时廓线正常但确实无云 → 应当切段。
//
// 与上一条形成对照：缺测与真无云必须被区别对待，否则修复就白做了。
func TestCloudSeaMerge_GenuineNoCloudSplits(t *testing.T) {
	cfg := config.Default()
	resp := makeCloudSeaResp(t)
	const missIdx = 3 // 当地时间 02:00

	// 只把 900hPa 云量清零，温湿与位势高都在 → 廓线可用，但几何上确实无云海。
	resp.Series["cloud_cover_900hPa"][missIdx] = model.Num(0)
	lvs := profile.BuildProfile(resp.LevelValues(missIdx), cfg.Thresh)
	t.Logf("[真无云] idx=%d(02:00) 层数=%d → ProfileUsable=%v（有数据、无云海）",
		missIdx, len(lvs), ProfileUsable(lvs))
	if !ProfileUsable(lvs) {
		t.Fatalf("fixture 构造失败：idx=%d 应为「廓线可用但无云」，实际不可用", missIdx)
	}

	eps := CollectCloudSeaEpisodesForNight(mergeSite, resp, mergeNight, cfg)
	total := dumpEpisodes(t, "真无云·02:00廓线正常但900hPa无云", eps)

	// 真无云是真正的断点，必须切段。
	if len(eps) != 2 || total != 7 {
		t.Fatalf("真无云场景应有 2 段 / 7 时次，实际 %d 段 / %d 时次", len(eps), total)
	}
	for i, e := range eps {
		if e.MissingHours != 0 {
			t.Errorf("段%d 无缺测，MissingHours 应为 0，实际 %d", i, e.MissingHours)
		}
	}
	t.Logf("[对照] 缺测保持 1 段 + MissingHours=1，真无云切成 2 段 —— 两者已被区别对待")
}

// TestCloudSeaMerge_DownstreamConfidence 下游影响：缺测 1h 时可信度不得因「被切段」而降级。
//
// 修复前：缺测把 1 段切成 2 段并使 CloudSeaHours 8→7，可信度从「极高」掉到「高」。
// 修复后：段数保持 1；时长仍为 7（缺测本就不该计入），故 7h 落在「高」档——
// 这是诚实的降级（我们确实不知道那 1 小时有没有云海），而不是算法缺陷。
func TestCloudSeaMerge_DownstreamConfidence(t *testing.T) {
	cfg := config.Default()
	sunriseDate := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)

	base := makeCloudSeaResp(t)
	r0 := BuildSunriseReport(mergeSite, base, mergeNight, sunriseDate, cfg, 28800, 30)
	t.Logf("[下游·基线]  CloudSeaHours=%d 段数=%d → 可信度=%s（%s）",
		r0.CloudSeaHours, len(r0.Episodes), r0.Confidence, r0.ConfidenceNote)

	miss := makeCloudSeaResp(t)
	blankLevelProfileAt(miss, 3)
	r1 := BuildSunriseReport(mergeSite, miss, mergeNight, sunriseDate, cfg, 28800, 30)
	t.Logf("[下游·缺测1h] CloudSeaHours=%d 段数=%d → 可信度=%s（%s）",
		r1.CloudSeaHours, len(r1.Episodes), r1.Confidence, r1.ConfidenceNote)

	// 关键回归点：段数不再因缺测而膨胀。
	if len(r1.Episodes) != 1 {
		t.Errorf("缺测时段数=%d，期望 1（缺测不得切段）", len(r1.Episodes))
	}
	if r1.CloudSeaHours != 7 {
		t.Errorf("缺测时 CloudSeaHours=%d，期望 7（缺测不计入时长）", r1.CloudSeaHours)
	}
	// 缺测必须被如实告知，不能只在时段里默默少 1 小时。
	if r1.ConfidenceNote == r0.ConfidenceNote {
		t.Errorf("可信度说明未体现缺测：\n  基线=%q\n  缺测=%q",
			r0.ConfidenceNote, r1.ConfidenceNote)
	}
	t.Logf("[下游] 可信度 %s → %s（段数保持 1，时长 8→7 为诚实降级）",
		r0.Confidence, r1.Confidence)
}

// ---------------------------------------------------------------------------
// 分辨率自适应
// ---------------------------------------------------------------------------

// TestCloudSeaMerge_3HourResolution 时间轴为 3 小时分辨率时的合并行为。
//
// 修复前：maxGapHours=1 硬编码 + End 硬编码 +1h，连续 3 个时次被切成 3 段各 1h，
// 且末段 End 少算 2h（真实覆盖 9h 被报成 3h）。
func TestCloudSeaMerge_3HourResolution(t *testing.T) {
	cfg := config.Default()
	resp := makeCloudSeaResp3h(t)

	for i, ts := range resp.Times {
		t.Logf("[3h] Times[%d]=%s hour=%d nightID=%s inWindow=%v",
			i, ts.Format(time.RFC3339), ts.Hour(), NightIDOf(ts),
			InNightWindow(ts.Hour(), cfg.Window))
	}

	eps := CollectCloudSeaEpisodesForNight(mergeSite, resp, mergeNight, cfg)
	total := dumpEpisodes(t, "3h分辨率·连续3个时次有云海", eps)

	// 真实覆盖：首时次 23:00 → 末时次 05:00 的结束时刻 08:00，共 9h。
	realStart := resp.Times[0]
	realEnd := resp.Times[len(resp.Times)-1].Add(3 * time.Hour)
	t.Logf("[3h] 真实覆盖 %s → %s（跨度 %.0fh）；代码报告累计 HoursCount=%d",
		realStart.Format("15:04"), realEnd.Format("15:04"),
		realEnd.Sub(realStart).Hours(), total)

	if len(eps) != 1 {
		t.Fatalf("3h 分辨率下连续 3 个时次被切成 %d 段，期望 1 段（间隔应由真实采样间隔推导）",
			len(eps))
	}
	if total != 3 {
		t.Errorf("HoursCount=%d，期望 3（3 个时次，非 9 小时）", total)
	}
	last := eps[len(eps)-1]
	if !last.End.Equal(realEnd) {
		t.Errorf("末段 End=%s，期望 %s（末时次 + 实际间隔 3h）",
			last.End.Format("15:04"), realEnd.Format("15:04"))
	}
	if !last.Start.Equal(realStart) {
		t.Errorf("首段 Start=%s，期望 %s", last.Start.Format("15:04"), realStart.Format("15:04"))
	}
}

// TestSeriesInterval 锁死采样间隔推导：取相邻时刻的最小正间隔，异常输入退回 1h。
func TestSeriesInterval(t *testing.T) {
	base := time.Date(2026, 9, 15, 23, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		times []time.Time
		want  time.Duration
	}{
		{"空序列→退回1h", nil, time.Hour},
		{"单点→退回1h", []time.Time{base}, time.Hour},
		{"1h分辨率", []time.Time{base, base.Add(time.Hour), base.Add(2 * time.Hour)}, time.Hour},
		{"3h分辨率", []time.Time{base, base.Add(3 * time.Hour), base.Add(6 * time.Hour)}, 3 * time.Hour},
		{"30min分辨率", []time.Time{base, base.Add(30 * time.Minute)}, 30 * time.Minute},
		{"乱序/零间隔→退回1h", []time.Time{base, base}, time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := seriesInterval(c.times); got != c.want {
				t.Fatalf("seriesInterval = %v，期望 %v", got, c.want)
			}
		})
	}
}
