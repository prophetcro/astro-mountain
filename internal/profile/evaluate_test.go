package profile

import (
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func testThresholds() config.Thresholds {
	return config.Default().Thresh
}

func testSite() model.Site {
	return model.Site{Name: "牵牛岗", Lat: 30.0260, Lon: 119.0070, Alt: 1489.9}
}

func TestEvaluateHourAllMissingIsNoData(t *testing.T) {
	th := testThresholds()

	levelValues := make(map[int]model.RawLevel, len(PressureLevels))
	for _, p := range PressureLevels {
		levelValues[p] = model.RawLevel{
			CC: model.Missing(),
			GH: model.Missing(),
			RH: model.Missing(),
		}
	}
	levels := BuildProfile(levelValues, th)
	if len(levels) != 0 {
		t.Fatalf("全缺测时廓线应为空，实际拿到 %d 层", len(levels))
	}
	layers := DetectLayers(levels, th)
	if len(layers) != 0 {
		t.Fatalf("空廓线不应反演出云层，实际 %d 层", len(layers))
	}

	ev := EvaluateHour(testSite(), model.Surface{}, layers, levels, th)

	if ev.Rating != RATING_NODATA {
		t.Fatalf("全缺测的评级为 %q，必须是 %q", ev.Rating, RATING_NODATA)
	}
	if ev.Rating == RATING_CLEAR {
		t.Fatal("缺测安全红线被击穿：全缺测被判成了「通透」，用户会白跑一趟")
	}
	if ev.Relation != REL_NODATA {
		t.Fatalf("全缺测的关系为 %q，必须是 %q", ev.Relation, REL_NODATA)
	}
	if ev.KeyLayer != nil {
		t.Fatal("全缺测不应给出决定层")
	}
	if !strings.Contains(ev.Note, "缺测") {
		t.Fatalf("判断说明应说明缺测原因，实际 %q", ev.Note)
	}
}

func TestEvaluateHourEmptyLevelsShortCircuits(t *testing.T) {
	th := testThresholds()
	surface := model.Surface{
		Temperature2m:      model.Num(18.0),
		DewPoint2m:         model.Num(17.5),
		RelativeHumidity2m: model.Num(97.0),
		Visibility:         model.Num(20000),
	}
	ev := EvaluateHour(testSite(), surface, nil, nil, th)
	if ev.Rating != RATING_NODATA {
		t.Fatalf("空廓线 + 地面有数据，评级为 %q，必须仍是 %q", ev.Rating, RATING_NODATA)
	}
}

func TestEvaluateHourTrueClear(t *testing.T) {
	th := testThresholds()
	levelValues := map[int]model.RawLevel{
		1000: {CC: model.Num(0), GH: model.Num(110), RH: model.Num(55)},
		975:  {CC: model.Num(0), GH: model.Num(330), RH: model.Num(50)},
		950:  {CC: model.Num(2), GH: model.Num(560), RH: model.Num(48)},
		925:  {CC: model.Num(1), GH: model.Num(800), RH: model.Num(45)},
		900:  {CC: model.Num(0), GH: model.Num(1050), RH: model.Num(40)},
		850:  {CC: model.Num(0), GH: model.Num(1560), RH: model.Num(35)},
		800:  {CC: model.Num(0), GH: model.Num(2100), RH: model.Num(30)},
		700:  {CC: model.Num(0), GH: model.Num(3200), RH: model.Num(25)},
	}
	levels := BuildProfile(levelValues, th)
	if len(levels) == 0 {
		t.Fatal("有效数据却得到空廓线")
	}
	layers := DetectLayers(levels, th)
	surface := model.Surface{
		Temperature2m:      model.Num(15.0),
		DewPoint2m:         model.Num(5.0),
		RelativeHumidity2m: model.Num(52.0),
		Visibility:         model.Num(24000),
		CloudCoverLow:      model.Num(0),
	}
	ev := EvaluateHour(testSite(), surface, layers, levels, th)
	if ev.Rating != RATING_OK {
		t.Fatalf("晴空廓线评级为 %q（说明 %q），want %q", ev.Rating, ev.Note, RATING_OK)
	}
	if ev.Relation != REL_CLEAR {
		t.Fatalf("晴空廓线关系为 %q，want %q", ev.Relation, REL_CLEAR)
	}
}

func TestEvaluateHourSeaBelow(t *testing.T) {
	th := testThresholds()

	levelValues := map[int]model.RawLevel{
		1000: {CC: model.Num(10), GH: model.Num(110), RH: model.Num(70)},
		975:  {CC: model.Num(95), GH: model.Num(330), RH: model.Num(98)},
		950:  {CC: model.Num(98), GH: model.Num(560), RH: model.Num(99)},
		925:  {CC: model.Num(10), GH: model.Num(800), RH: model.Num(60)},
		900:  {CC: model.Num(0), GH: model.Num(1050), RH: model.Num(40)},
		850:  {CC: model.Num(0), GH: model.Num(1560), RH: model.Num(30)},
		800:  {CC: model.Num(0), GH: model.Num(2100), RH: model.Num(25)},
		700:  {CC: model.Num(0), GH: model.Num(3200), RH: model.Num(20)},
	}
	levels := BuildProfile(levelValues, th)
	layers := DetectLayers(levels, th)
	if len(layers) == 0 {
		t.Fatal("饱和层未被识别为云层")
	}
	ev := EvaluateHour(testSite(), model.Surface{
		Temperature2m:      model.Num(12.0),
		DewPoint2m:         model.Num(4.0),
		RelativeHumidity2m: model.Num(58.0),
		Visibility:         model.Num(30000),
	}, layers, levels, th)

	if ev.Relation != REL_SEA_BELOW {
		t.Fatalf("关系为 %q，want %q（说明 %q）", ev.Relation, REL_SEA_BELOW, ev.Note)
	}
	if ev.Rating == RATING_NODATA {
		t.Fatal("有效廓线不应判成无数据")
	}
}

func TestLevelCloudyRHOnlyFallback(t *testing.T) {
	th := testThresholds()
	humidNoCloud := Level{Pressure: 925, Height: 800, CC: model.Num(20), RH: model.Num(95)}
	if humidNoCloud.Cloudy(th) {
		t.Fatal("云量 20% + RH 95% 被判成有云：RH 兜底不该与云量并列触发")
	}
	ccMissing := Level{Pressure: 925, Height: 800, CC: model.Missing(), RH: model.Num(95)}
	if !ccMissing.Cloudy(th) {
		t.Fatal("云量缺测 + RH 95% 应触发 RH 兜底判为有云")
	}
	unknown := Level{Pressure: 925, Height: 800, CC: model.Missing(), RH: model.Missing()}
	if unknown.Known() {
		t.Fatal("CC/RH 全缺测的层必须是 unknown")
	}
}

func clearLevelValues() map[int]model.RawLevel {
	return map[int]model.RawLevel{
		1000: {CC: model.Num(0), GH: model.Num(110), RH: model.Num(45)},
		975:  {CC: model.Num(0), GH: model.Num(330), RH: model.Num(42)},
		950:  {CC: model.Num(0), GH: model.Num(560), RH: model.Num(40)},
		925:  {CC: model.Num(0), GH: model.Num(800), RH: model.Num(38)},
		900:  {CC: model.Num(0), GH: model.Num(1050), RH: model.Num(35)},
		850:  {CC: model.Num(0), GH: model.Num(1560), RH: model.Num(30)},
		800:  {CC: model.Num(0), GH: model.Num(2100), RH: model.Num(28)},
		700:  {CC: model.Num(0), GH: model.Num(3200), RH: model.Num(25)},
	}
}

func evalClearWith(t *testing.T, surface model.Surface) Evaluation {
	t.Helper()
	th := testThresholds()
	levels := BuildProfile(clearLevelValues(), th)
	if len(levels) == 0 {
		t.Fatal("晴空廓线不应为空——底座失效，后续断言无意义")
	}
	layers := DetectLayers(levels, th)
	if len(layers) != 0 {
		t.Fatalf("晴空廓线不应反演出云层，实际 %d 层——底座失效", len(layers))
	}
	return EvaluateHour(testSite(), surface, layers, levels, th)
}

func baseClearSurface() model.Surface {
	return model.Surface{
		Temperature2m:      model.Num(15.0),
		DewPoint2m:         model.Num(2.0),
		RelativeHumidity2m: model.Num(45.0),
		Visibility:         model.Num(30000),
		CloudCoverLow:      model.Num(0),
	}
}

func TestEvaluateHourPrecipRain(t *testing.T) {
	surface := baseClearSurface()
	surface.Precipitation = model.Num(1.2)

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_BAD {
		t.Fatalf("降水 1.2mm 的评级为 %q，必须是 %q（说明：%s）", ev.Rating, RATING_BAD, ev.Note)
	}
	if ev.Rating == RATING_CLEAR {
		t.Fatal("降水时次被判成「通透」：下雨天把用户送上山，是本工具最危险的失效模式")
	}
	if !strings.Contains(ev.Note, "不宜拍摄") {
		t.Fatalf("降水说明未包含「不宜拍摄」，实际：%s", ev.Note)
	}
	if !strings.Contains(ev.Note, "1.2mm") {
		t.Fatalf("降水说明未给出降水量，用户无从判断严重程度，实际：%s", ev.Note)
	}
}

func TestEvaluateHourPrecipThunderstorm(t *testing.T) {
	surface := baseClearSurface()
	surface.WeatherCode = model.Num(model.WMOCodeThunderstorm)

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_BAD {
		t.Fatalf("雷暴(95)的评级为 %q，必须是 %q（说明：%s）", ev.Rating, RATING_BAD, ev.Note)
	}
	if !strings.Contains(ev.Note, "雷暴") {
		t.Fatalf("雷暴说明未点名「雷暴」，实际：%s", ev.Note)
	}
	if !strings.Contains(ev.Note, "不宜拍摄") {
		t.Fatalf("雷暴说明未包含「不宜拍摄」，实际：%s", ev.Note)
	}
}

func TestEvaluateHourPrecipCodeWithoutAmount(t *testing.T) {
	surface := baseClearSurface()
	surface.WeatherCode = model.Num(model.WMOCodeShowerSlight)
	surface.Precipitation = model.Missing()

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_BAD {
		t.Fatalf("阵雨码 80(降水量缺测)的评级为 %q，必须是 %q（说明：%s）",
			ev.Rating, RATING_BAD, ev.Note)
	}
	if !strings.Contains(ev.Note, "80") {
		t.Fatalf("说明未给出天气码，实际：%s", ev.Note)
	}
}

func TestEvaluateHourHighThinCloud(t *testing.T) {
	surface := baseClearSurface()
	surface.CloudCoverHigh = model.Num(100)

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_WARN {
		t.Fatalf("高云量 100%% 的评级为 %q，want %q（说明：%s）", ev.Rating, RATING_WARN, ev.Note)
	}
	if ev.Rating == RATING_CLEAR {
		t.Fatal("高云盖顶被判成「通透」：3km 以上的云剖面看不到，但用户抬头看得到")
	}
	if !strings.Contains(ev.Note, "高云量") {
		t.Fatalf("降级说明未提及高云量，用户不知为何被降级，实际：%s", ev.Note)
	}

	if ev.Relation != REL_CLEAR {
		t.Fatalf("高云降级不应改写剖面关系，实际 %q want %q", ev.Relation, REL_CLEAR)
	}
}

func TestEvaluateHourHighThinCloudBelowThreshold(t *testing.T) {
	surface := baseClearSurface()
	surface.CloudCoverHigh = model.Num(30)

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_OK {
		t.Fatalf("高云量 30%% 不应降级，实际 %q（说明：%s）", ev.Rating, ev.Note)
	}
}

func TestEvaluateHourPrecipOverridesHighCloud(t *testing.T) {
	surface := baseClearSurface()
	surface.CloudCoverHigh = model.Num(100)
	surface.Precipitation = model.Num(2.4)
	surface.WeatherCode = model.Num(model.WMOCodeRainSlight)

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_BAD {
		t.Fatalf("降水+高云同时成立时评级为 %q，必须取更严的 %q（说明：%s）",
			ev.Rating, RATING_BAD, ev.Note)
	}
	if !strings.Contains(ev.Note, "不宜拍摄") {
		t.Fatalf("说明未包含「不宜拍摄」，实际：%s", ev.Note)
	}
}

func TestEvaluateHourMidThinCloud(t *testing.T) {
	surface := baseClearSurface()
	surface.CloudCoverMid = model.Num(85)
	surface.CloudCoverHigh = model.Num(20)

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_WARN {
		t.Fatalf("中云量 85%%(高云 20%%)的评级为 %q，want %q（说明：%s）",
			ev.Rating, RATING_WARN, ev.Note)
	}
	if ev.Rating == RATING_CLEAR {
		t.Fatal("中云盖顶被判成「通透」：3–8km 的中云完全在剖面之外，剖面扫不到，但用户抬头看得到")
	}
	if !strings.Contains(ev.Note, "85") {
		t.Fatalf("降级说明未给出触发降级的中云量 85%%，用户不知为何被降级，实际：%s", ev.Note)
	}

	if ev.Relation != REL_CLEAR {
		t.Fatalf("中云降级不应改写剖面关系，实际 %q want %q", ev.Relation, REL_CLEAR)
	}
}

func TestEvaluateHourMidAndHighBelowThreshold(t *testing.T) {
	surface := baseClearSurface()
	surface.CloudCoverMid = model.Num(60)
	surface.CloudCoverHigh = model.Num(45)

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_OK {
		t.Fatalf("中云 60%%/高云 45%% 均未达阈值，不应降级，实际 %q（说明：%s）", ev.Rating, ev.Note)
	}
}

func TestEvaluateHourMidMissingHighFull(t *testing.T) {
	surface := baseClearSurface()
	surface.CloudCoverMid = model.Missing()
	surface.CloudCoverHigh = model.Num(100)

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_WARN {
		t.Fatalf("中云缺测但高云 100%% 的评级为 %q，want %q（说明：%s）",
			ev.Rating, RATING_WARN, ev.Note)
	}
	if !strings.Contains(ev.Note, "100") {
		t.Fatalf("降级说明未给出高云量 100%%，实际：%s", ev.Note)
	}
}

func TestEvaluateHourMidAndHighBothFull(t *testing.T) {
	surface := baseClearSurface()
	surface.CloudCoverMid = model.Num(88)
	surface.CloudCoverHigh = model.Num(95)

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_WARN {
		t.Fatalf("中云 88%%/高云 95%% 的评级为 %q，want %q（说明：%s）",
			ev.Rating, RATING_WARN, ev.Note)
	}
	if !strings.Contains(ev.Note, "95") {
		t.Fatalf("说明应给出高云量 95%%，实际：%s", ev.Note)
	}
	if strings.Contains(ev.Note, "183") {
		t.Fatalf("中/高云量被求和成 183%%，物理上不成立，实际：%s", ev.Note)
	}
}

func TestEvaluateHourNoPrecipStaysClear(t *testing.T) {
	surface := baseClearSurface()
	surface.Precipitation = model.Num(0)
	surface.WeatherCode = model.Num(0)
	surface.CloudCoverMid = model.Num(3)
	surface.CloudCoverHigh = model.Num(5)

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_OK {
		t.Fatalf("晴夜被误伤成 %q（说明：%s）", ev.Rating, ev.Note)
	}
	if ev.Relation != REL_CLEAR {
		t.Fatalf("晴夜关系为 %q，want %q", ev.Relation, REL_CLEAR)
	}
}

func TestEvaluateHourMidFullHighMissing(t *testing.T) {
	surface := baseClearSurface()
	surface.CloudCoverMid = model.Num(85)
	surface.CloudCoverHigh = model.Missing()

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_WARN {
		t.Fatalf("中云 85%%、高云缺测的评级为 %q，want %q（说明：%s）",
			ev.Rating, RATING_WARN, ev.Note)
	}
	if ev.Rating == RATING_CLEAR {
		t.Fatal("高云缺测把中云满的时次拉回「通透」：缺测被当成 0 参与取大值了")
	}
	if !strings.Contains(ev.Note, "85") {
		t.Fatalf("降级说明未给出触发降级的中云量 85%%，实际：%s", ev.Note)
	}
}

func TestEvaluateHourMidAndHighBothMissing(t *testing.T) {
	surface := baseClearSurface()
	surface.CloudCoverMid = model.Missing()
	surface.CloudCoverHigh = model.Missing()

	ev := evalClearWith(t, surface)

	if ev.Rating != RATING_OK {
		t.Fatalf("中/高云均缺测却被降级为 %q：缺测被当成了肯定判据（说明：%s）",
			ev.Rating, ev.Note)
	}
	if strings.Contains(ev.Note, "薄云盖顶") {
		t.Fatalf("中/高云均缺测却给出「薄云盖顶」说明，判据在无数据时凭空成立，实际：%s", ev.Note)
	}
}

func TestMaxValid(t *testing.T) {
	cases := []struct {
		name      string
		a, b      model.OptFloat
		wantValid bool
		wantV     float64
		guard     string
	}{
		{"都有效取较大者(a>b)", model.Num(85), model.Num(20), true, 85,
			"取大者是对「头顶最厚那层遮挡」的保守下限估计"},
		{"都有效取较大者(b>a)", model.Num(20), model.Num(85), true, 85,
			"比较必须与参数顺序无关"},
		{"都有效且相等", model.Num(80), model.Num(80), true, 80,
			"相等时不能落进 default 变成缺测"},
		{"仅 a 有效", model.Num(85), model.Missing(), true, 85,
			"缺测的一侧不参与比较，绝不能当成 0 把有效值压下去"},
		{"仅 b 有效", model.Missing(), model.Num(85), true, 85,
			"对称于上一格"},
		{"都缺测", model.Missing(), model.Missing(), false, 0,
			"必须返回 Missing 而非 Num(0)：「没有数据」不是「云量为 0」"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := maxValid(tc.a, tc.b)

			if got.Valid != tc.wantValid {
				t.Fatalf("Valid = %v, want %v —— %s", got.Valid, tc.wantValid, tc.guard)
			}

			if tc.wantValid && got.V != tc.wantV {
				t.Fatalf("V = %v, want %v —— %s", got.V, tc.wantV, tc.guard)
			}
		})
	}
}

func TestEvaluateHourThinVeilThresholdBoundary(t *testing.T) {
	th := testThresholds()
	midThr, highThr := th.MidCloudVeilCC, th.HighCloudThinVeilCC

	if midThr <= 0 || highThr <= 0 {
		t.Fatalf("阈值未正确加载：mid=%.1f high=%.1f", midThr, highThr)
	}

	cases := []struct {
		name  string
		mid   model.OptFloat
		high  model.OptFloat
		want  string
		guard string
	}{
		{"中云恰好等于 mid_cloud_veil_cc", model.Num(midThr), model.Missing(), RATING_WARN,
			"阈值是「>=」：恰好等于这一格必须降级，实测漏判就落在 80–89%"},
		{"中云差 0.1 未到 mid_cloud_veil_cc", model.Num(midThr - 0.1), model.Missing(), RATING_OK,
			"差 0.1 不应降级，否则阈值形同虚设"},
		{"高云恰好等于 high_cloud_thin_veil_cc", model.Missing(), model.Num(highThr), RATING_WARN,
			"高云侧同样走「>=」，不能与中云侧不一致"},
		{"高云差 0.1 未到 high_cloud_thin_veil_cc", model.Missing(), model.Num(highThr - 0.1), RATING_OK,
			"差 0.1 不应降级"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			surface := baseClearSurface()
			surface.CloudCoverMid = tc.mid
			surface.CloudCoverHigh = tc.high

			ev := evalClearWith(t, surface)

			if ev.Rating != tc.want {
				t.Fatalf("评级为 %q，want %q —— %s（说明：%s）",
					ev.Rating, tc.want, tc.guard, ev.Note)
			}
		})
	}
}

func TestEvaluateHourVeilNoteIdentifiesCloudLayer(t *testing.T) {
	cases := []struct {
		name        string
		mid         model.OptFloat
		high        model.OptFloat
		wantRating  string
		contains    []string
		notContains []string
		guard       string
	}{
		{
			name: "仅中云过阈", mid: model.Num(100), high: model.Num(10),
			wantRating: RATING_WARN,
			contains:   []string{"中云量", "100", "3–8km"},

			notContains: []string{"高云量", "8km 以上"},
			guard: "冷湖镇 2026-08-09 23:00 实测样本：low=0%/mid=100%/high=58%，" +
				"触发者是 3–8km 中云，文案必须点名中云并给出高度区间",
		},
		{
			name: "仅高云过阈", mid: model.Num(10), high: model.Num(100),
			wantRating:  RATING_WARN,
			contains:    []string{"高云量", "100", "8km 以上", "卷云"},
			notContains: []string{"中云量"},
			guard: "牵牛岗 2026-08-12 23:00 实测样本：low=32%/mid=14%/high=100%，" +
				"触发者是 8km 以上卷云，文案必须点名高云",
		},
		{
			name: "中云与高云同时过阈", mid: model.Num(90), high: model.Num(95),
			wantRating:  RATING_WARN,
			contains:    []string{"中云量 90", "高云量 95"},
			notContains: []string{"185"},
			guard: "两层互不重叠、是两个独立事件：同时过阈时两条提示都要出现，" +
				"只出一条会让用户漏掉另一层云；也绝不能把两个覆盖率相加",
		},
		{
			name: "中云与高云都不过阈", mid: model.Num(60), high: model.Num(45),
			wantRating:  RATING_OK,
			contains:    nil,
			notContains: []string{"中云量", "高云量"},
			guard:       "两侧都不过阈必须保持通透，否则判据退化成「永远降级」的噪声",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			surface := baseClearSurface()
			surface.CloudCoverMid = tc.mid
			surface.CloudCoverHigh = tc.high

			ev := evalClearWith(t, surface)

			if ev.Rating != tc.wantRating {
				t.Fatalf("评级为 %q，want %q —— %s（说明：%s）",
					ev.Rating, tc.wantRating, tc.guard, ev.Note)
			}
			for _, sub := range tc.contains {
				if !strings.Contains(ev.Note, sub) {
					t.Fatalf("说明缺少 %q —— %s（实际：%s）", sub, tc.guard, ev.Note)
				}
			}
			for _, sub := range tc.notContains {
				if strings.Contains(ev.Note, sub) {
					t.Fatalf("说明不应出现 %q —— %s（实际：%s）", sub, tc.guard, ev.Note)
				}
			}
		})
	}
}

func TestEvaluateHourMidAndHighVeilNotesDiffer(t *testing.T) {
	const cc = 100.0

	midSurface := baseClearSurface()
	midSurface.CloudCoverMid = model.Num(cc)
	midSurface.CloudCoverHigh = model.Missing()
	midEv := evalClearWith(t, midSurface)

	highSurface := baseClearSurface()
	highSurface.CloudCoverMid = model.Missing()
	highSurface.CloudCoverHigh = model.Num(cc)
	highEv := evalClearWith(t, highSurface)

	if midEv.Rating != RATING_WARN || highEv.Rating != RATING_WARN {
		t.Fatalf("前置条件不成立，两侧都应降级为 %q：mid=%q high=%q",
			RATING_WARN, midEv.Rating, highEv.Rating)
	}
	if midEv.Note == highEv.Note {
		t.Fatalf(`中云侧与高云侧给出了完全相同的说明：%s
两者是不同高度层（3–8km vs 8km 以上）、对星野的影响天差地别，
共用一句文案等于告诉用户「有云」却不说在哪层——本轮改动就是为了修掉它。`,
			midEv.Note)
	}
}

func TestEvaluateHourEmptyLevelsPrecipStillBad(t *testing.T) {
	th := testThresholds()
	surface := model.Surface{
		Precipitation: model.Num(2.0),
		WeatherCode:   model.Num(model.WMOCodeRainSlight),
	}

	ev := EvaluateHour(testSite(), surface, nil, nil, th)

	if ev.Rating != RATING_BAD {
		t.Fatalf("廓线空 + 降水 2mm 的评级为 %q，必须是 %q（说明：%s）",
			ev.Rating, RATING_BAD, ev.Note)
	}
	if ev.Rating == RATING_NODATA {
		t.Fatal("廓线空把降水时次压回了无数据：下雨天用户据此上山，最危险的失效模式")
	}
	if !strings.Contains(ev.Note, "不宜拍摄") {
		t.Fatalf("降水说明未包含「不宜拍摄」，实际：%s", ev.Note)
	}
	if !strings.Contains(ev.Note, "2.0mm") {
		t.Fatalf("降水说明未给出降水量，用户无从判断严重程度，实际：%s", ev.Note)
	}
}

func TestEvaluateHourEmptyLevelsFogStillBad(t *testing.T) {
	th := testThresholds()
	surface := model.Surface{
		Visibility: model.Num(300),
	}
	ev := EvaluateHour(testSite(), surface, nil, nil, th)

	if ev.Rating != RATING_BAD {
		t.Fatalf("廓线空 + 能见度 300m 的评级为 %q，必须是 %q（说明：%s）",
			ev.Rating, RATING_BAD, ev.Note)
	}
	if ev.Rating == RATING_NODATA {
		t.Fatal("廓线空把有雾时次压回了无数据：用户据此上山，夜间山区有雾极其危险")
	}
	if !strings.Contains(ev.Note, "能见度") {
		t.Fatalf("雾说明未提及能见度，实际：%s", ev.Note)
	}
}

func TestEvaluateHourEmptyLevelsHazeWarns(t *testing.T) {
	th := testThresholds()
	surface := model.Surface{
		Visibility: model.Num(3000),
	}
	ev := EvaluateHour(testSite(), surface, nil, nil, th)

	if ev.Rating != RATING_WARN {
		t.Fatalf("廓线空 + 能见度 3000m 的评级为 %q，必须是 %q（说明：%s）",
			ev.Rating, RATING_WARN, ev.Note)
	}
	if ev.Rating == RATING_NODATA {
		t.Fatal("廓线空把轻雾时次压回了无数据")
	}
	if !strings.Contains(ev.Note, "轻雾") {
		t.Fatalf("轻雾说明未出现，实际：%s", ev.Note)
	}
}

// niucaoSite 是牛草山：alt=1442m，常年云海，机位常嵌在云层上沿。
func niucaoSite() model.Site {
	return model.Site{Name: "牛草山", Lat: 31.047, Lon: 116.259, Alt: 1442.0}
}

// 用构造好的云层直接喂给 EvaluateHour，绕开 BuildProfile/DetectLayers，
// 以便精准控制「机位在云中」的几何形态。levels 必须非空，否则会走缺测短路。
func evalWithLayers(t *testing.T, site model.Site, layers []CloudLayer, surface model.Surface) Evaluation {
	t.Helper()
	th := testThresholds()
	// 非空 levels 仅用于跳过 len(levels)==0 的缺测短路；主判定只看 layers。
	return EvaluateHour(site, surface, layers, []Level{{Pressure: 1000}}, th)
}

func clearInCloudSurface() model.Surface {
	return model.Surface{
		Temperature2m:      model.Num(12.0),
		DewPoint2m:         model.Num(6.0),
		RelativeHumidity2m: model.Num(70.0),
		Visibility:         model.Num(20000),
	}
}

// 牛草山形态：机位在云层顶部附近（云底在山脚 53m、云顶 1613m），
// 脚下是厚云海、头顶只剩薄云——应识别为「云海在脚下（机位在云中）」并降为⚠️。
func TestEvaluateHourInCloudButSeaBelow(t *testing.T) {
	layers := []CloudLayer{
		{BaseMSL: 53, TopMSL: 1613, MaxCC: 90},
	}
	ev := evalWithLayers(t, niucaoSite(), layers, clearInCloudSurface())

	if ev.Relation != REL_SEA_BELOW_IN_CLOUD {
		t.Fatalf("关系为 %q，应为 %q（说明 %q）", ev.Relation, REL_SEA_BELOW_IN_CLOUD, ev.Note)
	}
	if ev.Rating != RATING_WARN {
		t.Fatalf("高山云海形态评级为 %q，必须降为 %q（说明：%s）",
			ev.Rating, RATING_WARN, ev.Note)
	}
	if !strings.Contains(ev.Note, "云海在脚下（机位在云中）") {
		t.Fatalf("说明未点名「云海在脚下（机位在云中）」，实际：%s", ev.Note)
	}
	if strings.Contains(ev.Note, "无法拍摄") {
		t.Fatalf("高山云海形态不应给「无法拍摄」，实际：%s", ev.Note)
	}
}

// 机位埋在厚云中部（below 仅 142m、above 158m），并非云海在脚下——应保持🔴。
func TestEvaluateHourBuriedInCloudStaysBad(t *testing.T) {
	layers := []CloudLayer{
		{BaseMSL: 1300, TopMSL: 1600, MaxCC: 90},
	}
	ev := evalWithLayers(t, niucaoSite(), layers, clearInCloudSurface())

	if ev.Relation != REL_IN_CLOUD {
		t.Fatalf("关系为 %q，want %q（说明 %q）", ev.Relation, REL_IN_CLOUD, ev.Note)
	}
	if ev.Rating != RATING_BAD {
		t.Fatalf("机位埋在厚云中部却为 %q，必须保持 %q（说明：%s）",
			ev.Rating, RATING_BAD, ev.Note)
	}
	if !strings.Contains(ev.Note, "无法拍摄") {
		t.Fatalf("说明未给出「无法拍摄」，实际：%s", ev.Note)
	}
}

// 阈边境：脚下云够厚，但头顶云也极厚（above=1558m 远超上限），
// 这是实打实的厚云盖顶而非云海在脚下——应仍判🔴。
func TestEvaluateHourThickOverheadStaysBad(t *testing.T) {
	layers := []CloudLayer{
		{BaseMSL: 53, TopMSL: 3000, MaxCC: 90},
	}
	ev := evalWithLayers(t, niucaoSite(), layers, clearInCloudSurface())

	if ev.Relation != REL_IN_CLOUD {
		t.Fatalf("关系为 %q，want %q（说明 %q）", ev.Relation, REL_IN_CLOUD, ev.Note)
	}
	if ev.Rating != RATING_BAD {
		t.Fatalf("头顶厚云（above=1558m）却降为 %q，应仍 %q（说明：%s）",
			ev.Rating, RATING_BAD, ev.Note)
	}
}

// 辐射雾 + 脚下云海（机位在雾顶之上）：浓雾本应🔴，但辐射雾贴地、清晨散，
// 且机位在雾之上，应豁免为⚠️，并把说明改写为积极指引（可守候云隙破云/日出云海）。
func TestEvaluateHourRadiationFogSeaBelowExempts(t *testing.T) {
	layers := []CloudLayer{{BaseMSL: 53, TopMSL: 1613, MaxCC: 90}}
	surface := model.Surface{
		Temperature2m:      model.Num(12.0),
		DewPoint2m:         model.Num(11.8),
		RelativeHumidity2m: model.Num(98.0), // 触发雾 veto（RH 代理）
		Visibility:         model.Missing(),
		WindSpeed10m:       model.Num(1.0),  // 静风 → 辐射雾
		CloudCoverMid:      model.Num(10.0), // 晴夜少云
		CloudCoverHigh:     model.Num(5.0),
	}
	ev := evalWithLayers(t, niucaoSite(), layers, surface)

	if ev.Relation != REL_SEA_BELOW_IN_CLOUD {
		t.Fatalf("关系应为 %q，实际 %q（说明 %q）", REL_SEA_BELOW_IN_CLOUD, ev.Relation, ev.Note)
	}
	if ev.Rating != RATING_WARN {
		t.Fatalf("辐射雾+脚下云海评级为 %q，应豁免为 %q（说明：%s）", ev.Rating, RATING_WARN, ev.Note)
	}
	if !strings.Contains(ev.Note, "辐射雾贴地") {
		t.Fatalf("豁免后应给出辐射雾积极指引，实际：%s", ev.Note)
	}
	if strings.Contains(ev.Note, "无法拍摄") {
		t.Fatalf("辐射雾+脚下云海不应「无法拍摄」，实际：%s", ev.Note)
	}
}

// 反例1：辐射雾但机位埋在厚云中部（非脚下云海）→ 不豁免，仍🔴。
func TestEvaluateHourRadiationFogBuriedInCloudStillBad(t *testing.T) {
	layers := []CloudLayer{{BaseMSL: 1300, TopMSL: 1600, MaxCC: 90}}
	surface := model.Surface{
		RelativeHumidity2m: model.Num(98.0),
		Visibility:         model.Missing(),
		WindSpeed10m:       model.Num(1.0),
		CloudCoverMid:      model.Num(10.0),
		CloudCoverHigh:     model.Num(5.0),
	}
	ev := evalWithLayers(t, niucaoSite(), layers, surface)
	if ev.Relation != REL_IN_CLOUD {
		t.Fatalf("关系应为 %q，实际 %q（说明 %q）", REL_IN_CLOUD, ev.Relation, ev.Note)
	}
	if ev.Rating != RATING_BAD {
		t.Fatalf("辐射雾+机位埋厚云应为 %q（不豁免），实际 %q（说明：%s）",
			RATING_BAD, ev.Rating, ev.Note)
	}
}

// 反例2：有风（平流雾）+ 脚下云海 + 浓雾 → 不豁免，仍🔴。
func TestEvaluateHourAdvectionFogSeaBelowStillBad(t *testing.T) {
	layers := []CloudLayer{{BaseMSL: 53, TopMSL: 1613, MaxCC: 90}}
	surface := model.Surface{
		RelativeHumidity2m: model.Num(98.0),
		Visibility:         model.Missing(),
		WindSpeed10m:       model.Num(5.0), // 有风 → 平流雾
		CloudCoverMid:      model.Num(10.0),
		CloudCoverHigh:     model.Num(5.0),
	}
	ev := evalWithLayers(t, niucaoSite(), layers, surface)
	if ev.Rating != RATING_BAD {
		t.Fatalf("平流雾+脚下云海应为 %q（不豁免），实际 %q（说明：%s）",
			RATING_BAD, ev.Rating, ev.Note)
	}
}

// 反例3：静风但中/高云偏多（抑制辐射冷却）→ 不算辐射雾，不豁免，仍🔴。
func TestEvaluateHourRadiationFogButCloudyStillBad(t *testing.T) {
	layers := []CloudLayer{{BaseMSL: 53, TopMSL: 1613, MaxCC: 90}}
	surface := model.Surface{
		RelativeHumidity2m: model.Num(98.0),
		Visibility:         model.Missing(),
		WindSpeed10m:       model.Num(1.0),
		CloudCoverMid:      model.Num(80.0), // 厚云盖顶，非晴夜辐射冷却
		CloudCoverHigh:     model.Num(60.0),
	}
	ev := evalWithLayers(t, niucaoSite(), layers, surface)
	if ev.Rating != RATING_BAD {
		t.Fatalf("辐射雾但中高云多应为 %q（不豁免），实际 %q（说明：%s）",
			RATING_BAD, ev.Rating, ev.Note)
	}
}
