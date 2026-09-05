package report

import (
	"strings"
	"testing"
	"time"
)

// 本测试的时区口径刻意复刻生产数据的「时区混用」：
//   - 日出时刻：astro.SunriseTime 返回 FixedZone("local", +08:00) 下的当地墙钟；
//   - 云海时段：来自 api.Response.Times，是「加上 UTC 偏移后用 UTC 承载的当地墙钟」。
//
// 两者直接相减会整体错开 8 小时，正是「云海明明盖住日出却判成完全错开」的根源，
// 所以这里不统一成同一个时区，而是让每个用例都跑在真实的混用形态上。
var (
	testSunriseZone = time.FixedZone("local", 8*60*60)
	testAxisZone    = time.UTC
)

// testSunriseTime 造日出时刻（当地墙钟，挂在 FixedZone +08:00 上）。
func testSunriseTime(hour, minute int) time.Time {
	return time.Date(2026, 9, 6, hour, minute, 0, 0, testSunriseZone)
}

// testWallTime 造「剥掉时区的墙钟」（UTC 承载），用于直接比较窗口函数的返回值。
func testWallTime(hour, minute int) time.Time {
	return time.Date(2026, 9, 6, hour, minute, 0, 0, testAxisZone)
}

// testEpisode 造一段整点对齐的云海时段。endHour <= startHour 视为跨零点（次日）。
func testEpisode(startHour, endHour int) CloudSeaEpisode {
	return testEpisodeRange(startHour, 0, endHour, 0)
}

// testEpisodeRange 造一段带分钟精度的云海时段，只填窗口判定需要的 Start/End/HoursCount。
//
// 时段挂在 UTC 上承载当地墙钟（与 api.Response.Times 一致）；
// 起始时刻 ≥ 12 点的时段视为「夜里」起于日出前一日（2026-09-05），
// 跨零点后自然落在日出当天（2026-09-06），与真实报告里「22:00–01:00 + 日出 05:45」的口径一致。
func testEpisodeRange(startHour, startMin, endHour, endMin int) CloudSeaEpisode {
	day := 6
	if startHour >= 12 {
		day = 5
	}
	start := time.Date(2026, 9, day, startHour, startMin, 0, 0, testAxisZone)
	end := time.Date(2026, 9, day, endHour, endMin, 0, 0, testAxisZone)
	if !end.After(start) {
		end = end.AddDate(0, 0, 1)
	}
	return CloudSeaEpisode{
		Start:      start,
		End:        end,
		HoursCount: int(end.Sub(start)/time.Hour) + 1,
	}
}

// testSunriseResult 造一条站点结果：日出 05:45（FixedZone 墙钟），云海时段按入参给出。
func testSunriseResult(site string, eps ...CloudSeaEpisode) SunriseSiteResult {
	return testResultAt(site, 5, 45, eps...)
}

// testResultAt 造一条指定日出时刻的站点结果（东白山 05:41 这类非整点日出用）。
func testResultAt(site string, sunriseHour, sunriseMin int, eps ...CloudSeaEpisode) SunriseSiteResult {
	hours := 0
	for _, ep := range eps {
		hours += ep.HoursCount
	}
	return SunriseSiteResult{
		Site:          site,
		SunriseTime:   testSunriseTime(sunriseHour, sunriseMin),
		ArriveBy:      testSunriseTime(sunriseHour-1, sunriseMin-30),
		Episodes:      eps,
		CloudSeaHours: hours,
		DawnGlow:      "无",
		Confidence:    "中",
		Rating:        "⚠️ 测试用",
	}
}

func TestCloudSeaTimeLabel_NoEpisodes(t *testing.T) {
	r := testSunriseResult("无云海点")
	if got := cloudSeaTimeLabel(r); got != "" {
		t.Fatalf("无云海时段标签应为空串（由 orDash 渲染成 -），实际：%q", got)
	}
}

func TestCloudSeaTimeLabel_SingleAndTruncated(t *testing.T) {
	one := testSunriseResult("单点", testEpisode(4, 6))
	if got, want := cloudSeaTimeLabel(one), "04:00–06:00"; got != want {
		t.Fatalf("单段云海时段标签 = %q，期望 %q", got, want)
	}

	two := testSunriseResult("双点", testEpisode(22, 23), testEpisode(0, 1))
	if got, want := cloudSeaTimeLabel(two), "22:00–23:00, 00:00–01:00"; got != want {
		t.Fatalf("双段云海时段标签 = %q，期望 %q", got, want)
	}

	three := testSunriseResult("三点", testEpisode(22, 23), testEpisode(0, 1), testEpisode(3, 4))
	if got, want := cloudSeaTimeLabel(three), "22:00–23:00, 00:00–01:00, …"; got != want {
		t.Fatalf("三段云海时段标签 = %q，期望 %q", got, want)
	}
}

func TestSunriseWindowLabel_AllGrades(t *testing.T) {
	const before, after = 45, 30 // 窗口 = 05:00 ~ 06:15（日出 05:45）
	cases := []struct {
		name string
		eps  []CloudSeaEpisode
		want string
	}{
		// 云海 04:00–07:00 盖满 05:00–06:15。
		{"完整覆盖", []CloudSeaEpisode{testEpisode(4, 7)}, sunriseWindowFull},
		// 云海 04:00–06:00：含日出 05:45，但没盖到窗口末端 06:15。
		{"覆盖日出", []CloudSeaEpisode{testEpisode(4, 6)}, sunriseWindowSunrise},
		// 云海 05:00–05:30：与窗口相交，但日出那一刻已散。
		{"部分重叠", []CloudSeaEpisode{testEpisodeRange(5, 0, 5, 30)}, sunriseWindowPartial},
		// 云海半夜 22:00–03:00：与窗口 05:00 起一点不沾。
		{"完全错开", []CloudSeaEpisode{testEpisode(22, 3)}, sunriseWindowMissed},
		{"无云海", nil, sunriseWindowNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := testSunriseResult(tc.name, tc.eps...)
			if got := sunriseWindowLabel(r, before, after); got != tc.want {
				t.Fatalf("日出窗云海 = %q，期望 %q", got, tc.want)
			}
		})
	}
}

// TestSunriseWindowLabel_EndIsDissipation 守住端点语义：CloudSeaEpisode.End 是「消散时刻」，
// 云海段按左闭右开 [Start, End) 处理。
//
// 依据 core.mergeEpisodes：End = 最后一个有云海时次 + step，HoursCount 只数区间内的时次
// （实测 23:00→01:00 记 2h），明细表表头也写作「出现 / 消散」。
// 所以「04:00→05:00」表示 05:00 那一刻云海已确认消散，不该算进 05:00 才开始的拍摄窗口。
func TestSunriseWindowLabel_EndIsDissipation(t *testing.T) {
	const before, after = 45, 30 // 窗口 = 05:00 ~ 06:15（日出 05:45）
	winStart, winEnd := testWallTime(5, 0), testWallTime(6, 15)

	// 云海恰好在窗口起点消散 → 与窗口不相交，判完全错开。
	gone := testEpisode(3, 5)
	if got := sunriseWindowLabel(testSunriseResult("正好散", gone), before, after); got != sunriseWindowMissed {
		t.Fatalf("End == winStart（云海恰好在窗口起点消散）应判「%s」，实际：%q",
			sunriseWindowMissed, got)
	}
	if episodeOverlapsWindow(gone, winStart, winEnd) {
		t.Fatal("episodeOverlapsWindow 对 End == winStart 应返回 false（End 是消散时刻）")
	}
	if episodeContains(gone, testWallTime(5, 0)) {
		t.Fatal("episodeContains 对 End 端点应返回 false")
	}
	if !episodeContains(gone, testWallTime(4, 59)) {
		t.Fatal("episodeContains 对 End 前一分钟应返回 true")
	}

	// 消散时刻只要在窗口内（哪怕不到日出），就算部分重叠——不是笼统地「错开」。
	partial := testEpisodeRange(3, 0, 5, 30)
	if got := sunriseWindowLabel(testSunriseResult("散一半", partial), before, after); got != sunriseWindowPartial {
		t.Fatalf("云海 03:00→05:30 应判「%s」，实际：%q", sunriseWindowPartial, got)
	}
}

// TestSunriseWindowLabel_MixedTimeZones 守住时区口径：日出挂在 FixedZone(+0800)、
// 云海时段挂在「UTC 承载的当地墙钟」，这是生产数据的真实形态。
//
// 不做墙钟归一化时，「日出 05:45 / 云海 04:00–06:00」的绝对瞬间差被平移 8 小时，
// 云海会整个落到窗口之外、被判成「完全错开」——这正是端到端跑出来的假阴性。
func TestSunriseWindowLabel_MixedTimeZones(t *testing.T) {
	r := testSunriseResult("时区混用点", testEpisode(4, 6))
	ep := testEpisode(4, 6)

	// 前置校验：用例必须真的复现时区混用，否则本用例形同虚设。
	wallDelta := wallClockUTC(r.SunriseTime).Sub(wallClockUTC(ep.Start))
	if want := 105 * time.Minute; wallDelta != want { // 05:45 − 04:00
		t.Fatalf("墙钟差 = %v，期望 %v", wallDelta, want)
	}
	if r.SunriseTime.Sub(ep.Start) == wallDelta {
		t.Fatal("用例未复现生产数据的时区混用：绝对瞬间差与墙钟差相同（应相差 UTC+8）")
	}

	if got := sunriseWindowLabel(r, 45, 30); got != sunriseWindowSunrise {
		t.Fatalf("时区混用下日出窗云海 = %q，期望 %q", got, sunriseWindowSunrise)
	}
	// 墙钟读出来仍是 05:45 / 04:00–06:00，报告展示不受时区口径影响。
	if got, want := cloudSeaTimeLabel(r), "04:00–06:00"; got != want {
		t.Fatalf("云海时段标签 = %q，期望 %q", got, want)
	}
}

// TestSunriseWindowRankOrder 保证档位序与标签严格对应，且推荐排序的高位权重建立在它之上。
func TestSunriseWindowRankOrder(t *testing.T) {
	const before, after = 45, 30
	cases := []struct {
		eps  []CloudSeaEpisode
		want int
	}{
		{[]CloudSeaEpisode{testEpisode(4, 7)}, sunriseWindowRankFull},
		{[]CloudSeaEpisode{testEpisode(4, 6)}, sunriseWindowRankSunrise},
		{[]CloudSeaEpisode{testEpisodeRange(5, 0, 5, 30)}, sunriseWindowRankPartial},
		{[]CloudSeaEpisode{testEpisode(22, 3)}, sunriseWindowRankMissed},
		{nil, sunriseWindowRankNone},
	}
	for _, tc := range cases {
		r := testSunriseResult("档位", tc.eps...)
		if got := sunriseWindowRank(r, before, after); got != tc.want {
			t.Fatalf("sunriseWindowRank(%v) = %d，期望 %d", tc.eps, got, tc.want)
		}
	}
}

// TestBestSunriseSite_SunriseWindowBeatsMidnightDuration 是用户反馈缺陷的回归用例：
// 东白山式「半夜 5 小时云海、日出时早已散去」在旧的纯时长排序下会被推成最优；
// 改成日出窗优先后，宁可要「日出那一刻脚下有云海」的金华山式点位。
func TestBestSunriseSite_SunriseWindowBeatsMidnightDuration(t *testing.T) {
	// A：一段 04:00–06:00（3 个整点），日出 05:45 落在段内 → 覆盖日出。
	siteA := testSunriseResult("金华山·北山", testEpisode(4, 6))
	// B：半夜 22:00–03:00（6 个整点），日出 05:45 时云海早已散 → 完全错开。
	siteB := testSunriseResult("东白山", testEpisode(22, 3))
	// 让 B 在旧的排序口径下赢：时长更长（6 > 3），可信度也更高。
	siteB.Confidence = "极高"
	siteA.Confidence = "低"

	oldsScoreA := siteA.CloudSeaHours*10 + confidenceRank(siteA.Confidence)
	oldScoreB := siteB.CloudSeaHours*10 + confidenceRank(siteB.Confidence)
	if oldsScoreA >= oldScoreB {
		t.Fatalf("用例前置条件不成立：旧排序下 B 应获胜才能证明修复有效（A=%d, B=%d）",
			oldsScoreA, oldScoreB)
	}

	got := bestSunriseSite([]SunriseSiteResult{siteB, siteA}, 45, 30)
	if got != "金华山·北山" {
		t.Fatalf("综合推荐 = %q，期望 %q（日出窗有云海的点位必须胜过半夜长云海）",
			got, "金华山·北山")
	}
}

// TestSunriseSummaryTable_ReportedBugShapes 用用户反馈里那组真实数据端到端验证渲染层：
// 东白山「云海 22:00–23:00 + 00:00–01:00、日出 05:41」判完全错开，
// 金华山·北山「云海 04:00–06:00、日出 05:45」判覆盖日出，综合推荐给后者。
//
// 这组形状在实时预报里随模式更新会变（今天跑出来东白山多了一段 05:00–06:00），
// 所以把它固化成用例，保证判据本身不随某一次预报漂移。
func TestSunriseSummaryTable_ReportedBugShapes(t *testing.T) {
	db := testResultAt("东白山", 5, 41,
		testEpisodeRange(22, 0, 23, 0), testEpisodeRange(0, 0, 1, 0))
	jhs := testResultAt("金华山·北山", 5, 45, testEpisode(4, 6))

	table := strings.Join(sunriseSummaryTable([]SunriseSiteResult{db, jhs}, 45, 30), "\n")
	if !strings.Contains(table, "| 东白山 | 22:00–23:00, 00:00–01:00 | "+sunriseWindowMissed+" |") {
		t.Fatalf("东白山应判「%s」，实际表格：\n%s", sunriseWindowMissed, table)
	}
	if !strings.Contains(table, "| 金华山·北山 | 04:00–06:00 | "+sunriseWindowSunrise+" |") {
		t.Fatalf("金华山·北山应判「%s」，实际表格：\n%s", sunriseWindowSunrise, table)
	}
	if got := bestSunriseSite([]SunriseSiteResult{db, jhs}, 45, 30); got != "金华山·北山" {
		t.Fatalf("综合推荐 = %q，期望 %q", got, "金华山·北山")
	}
}

// TestBestSunriseSite_DawnGlowBreaksTies 校验同档内朝霞强度起作用（不打乱窗口档位优先级）。
func TestBestSunriseSite_DawnGlowBreaksTies(t *testing.T) {
	calm := testSunriseResult("无烧点", testEpisode(4, 6))
	calm.DawnGlow = "无"
	burn := testSunriseResult("大烧点", testEpisode(4, 6))
	burn.DawnGlow = "大烧"

	if got := bestSunriseSite([]SunriseSiteResult{calm, burn}, 45, 30); got != "大烧点" {
		t.Fatalf("同档内应按朝霞强度排序，实际推荐 = %q", got)
	}

	// 窗口档位仍压过朝霞：半夜云海 + 大烧，仍输给日出窗云海 + 无烧。
	midnightBurn := testSunriseResult("半夜大烧", testEpisode(22, 3))
	midnightBurn.DawnGlow = "大烧"
	if got := bestSunriseSite([]SunriseSiteResult{midnightBurn, calm}, 45, 30); got != "无烧点" {
		t.Fatalf("日出窗档位必须优先于朝霞，实际推荐 = %q", got)
	}
}

// TestSunriseSummaryTable_HasTimeColumns 校验汇总表真的带上了「云海时段 / 日出窗云海」两列，
// 且空值走 orDash 渲染成「-」，避免出现空单元格。
func TestSunriseSummaryTable_HasTimeColumns(t *testing.T) {
	rows := []SunriseSiteResult{
		testSunriseResult("有云海点", testEpisode(4, 6)),
		testSunriseResult("无云海点"),
	}
	table := strings.Join(sunriseSummaryTable(rows, 45, 30), "\n")

	for _, want := range []string{"云海时段", "日出窗云海", "04:00–06:00", sunriseWindowSunrise, sunriseWindowNone} {
		if !strings.Contains(table, want) {
			t.Fatalf("汇总表缺少 %q，实际表格：\n%s", want, table)
		}
	}
	// 表头列数必须与表体一致，否则 Markdown 表格会错位。
	header := sunriseSummaryHeaders()
	for _, line := range sunriseSummaryTable(rows, 45, 30) {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if n := strings.Count(line, "|") - 1; n != len(header) {
			t.Fatalf("表格行列数不一致：%q 有 %d 列，表头 %d 列", line, n, len(header))
		}
	}
}

// TestSunriseCaptureWindow_Fallback 校验配置值缺失（<=0）时回落到 45/30 分钟默认窗口。
// 返回值是剥掉时区的墙钟（UTC 承载），故与 testWallTime 比较。
func TestSunriseCaptureWindow_Fallback(t *testing.T) {
	r := testSunriseResult("兜底点")
	start, end := sunriseCaptureWindow(r, 0, 0)
	if want := testWallTime(5, 0); !start.Equal(want) {
		t.Fatalf("窗口起点 = %s，期望 %s（默认日出前 45min）", start, want)
	}
	if want := testWallTime(6, 15); !end.Equal(want) {
		t.Fatalf("窗口终点 = %s，期望 %s（默认日出后 30min）", end, want)
	}
}
