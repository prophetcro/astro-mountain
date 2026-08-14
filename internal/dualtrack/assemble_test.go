package dualtrack

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/model"
)

const testUTCOffset = 8.0

func localHour(day, hour int) time.Time {
	return time.Date(2026, 8, day, hour, 0, 0, 0, time.UTC)
}

func utcFor(day, hour int) time.Time {
	return localHour(day, hour).
		Add(-time.Duration(testUTCOffset * float64(time.Hour))).UTC()
}

func rowsAt(times ...time.Time) []model.HourRow {
	out := make([]model.HourRow, 0, len(times))
	for _, t := range times {
		out = append(out, model.HourRow{Time: t})
	}
	return out
}

func sampleAt(utc time.Time) HourInput {
	return HourInput{
		TimeUTC:    utc,
		CloudCover: num(0),
	}
}

func TestAssembleAlignsRowsOneToOne(t *testing.T) {
	rows := rowsAt(localHour(9, 20), localHour(9, 21), localHour(9, 22))
	samples := []HourInput{

		sampleAt(utcFor(9, 22)),
		sampleAt(utcFor(9, 20)),
	}

	res, err := Assemble("tianhuangping", testUTCOffset, 958.4,
		rows, samples, true, DatumAGL, nil)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}

	if len(res.Rows) != len(rows) {
		t.Fatalf("Rows 长度 %d，必须与 A 轨的 %d 相同", len(res.Rows), len(rows))
	}
	for i, r := range rows {
		if !res.Rows[i].TimeLocal.Equal(r.Time) {
			t.Errorf("第 %d 行 TimeLocal=%v，应与 A 轨的 %v 对齐",
				i, res.Rows[i].TimeLocal, r.Time)
		}

		wantUTC := r.Time.Add(-time.Duration(testUTCOffset * float64(time.Hour))).UTC()
		if !res.Rows[i].TimeUTC.Equal(wantUTC) {
			t.Errorf("第 %d 行 TimeUTC=%v，期望 %v", i, res.Rows[i].TimeUTC, wantUTC)
		}
	}

	if res.Rows[0].IsNoData() {
		t.Errorf("20 时有样本，不该 NODATA：%s", res.Rows[0].Note)
	}
	if got := res.Rows[1].NoDataReason; got != KeyMissing {
		t.Errorf("21 时无样本，应判 %s，得到 %s", KeyMissing, got)
	}
	if res.Rows[2].IsNoData() {
		t.Errorf("22 时有样本，不该 NODATA：%s", res.Rows[2].Note)
	}
}

func TestAssembleNeverHardcodesBeijingOffset(t *testing.T) {
	const offset = -5.0
	local := localHour(9, 20)
	wantUTC := local.Add(-time.Duration(offset * float64(time.Hour))).UTC()

	res, err := Assemble("somewhere", offset, 100,
		rowsAt(local), []HourInput{sampleAt(wantUTC)}, true, DatumAGL, nil)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}
	if res.Rows[0].IsNoData() {
		t.Fatalf("按 offset=%v 换算后本应对上样本，却判了 NODATA（%s）："+
			"实现很可能把 +8 写死了", offset, res.Rows[0].NoDataReason)
	}
	if !res.Rows[0].TimeUTC.Equal(wantUTC) {
		t.Errorf("TimeUTC=%v，期望 %v", res.Rows[0].TimeUTC, wantUTC)
	}
}

func TestAssembleDuplicateSamplesKeepFirst(t *testing.T) {
	utc := utcFor(9, 20)

	first := sampleAt(utc)
	second := sampleAt(utc)
	second.CloudCover = num(90)
	second.CloudBaseAGLM = num(500)

	res, err := Assemble("dup", testUTCOffset, 100,
		rowsAt(localHour(9, 20)), []HourInput{first, second}, true, DatumAGL, nil)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}

	if res.Rows[0].Rel != model.REL_CLEAR {
		t.Errorf("重复样本应保留第一条（CLEAR），得到 Rel=%s Reason=%s",
			res.Rows[0].Rel, res.Rows[0].NoDataReason)
	}
}

func TestOutOfHorizon(t *testing.T) {

	rows := rowsAt(localHour(10, 22), localHour(12, 20), localHour(12, 21))
	samples := []HourInput{
		sampleAt(utcFor(10, 22)),
		sampleAt(utcFor(10, 23)),
	}

	res, err := Assemble("kuocangshan", testUTCOffset, 1382.6,
		rows, samples, true, DatumAGL, nil)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}

	if res.Rows[0].IsNoData() {
		t.Errorf("窗口内且有样本，不该 NODATA：%s", res.Rows[0].Note)
	}
	for i := 1; i <= 2; i++ {
		if got := res.Rows[i].NoDataReason; got != OutOfHorizon {
			t.Errorf("第 %d 行超出样本最大时刻，应判 %s，得到 %s",
				i, OutOfHorizon, got)
		}

		if !strings.Contains(res.Rows[i].Note, "5 天") {
			t.Errorf("第 %d 行文案应说明 5 天上限，实际：%s", i, res.Rows[i].Note)
		}
	}
	if n := res.CountByReason(OutOfHorizon); n != 2 {
		t.Errorf("CountByReason(OutOfHorizon)=%d，期望 2", n)
	}
}

func TestRoundQuotaDownOverridesEverything(t *testing.T) {
	rows := rowsAt(localHour(9, 20), localHour(9, 21))

	res, err := Assemble("quota", testUTCOffset, 100,
		rows, []HourInput{sampleAt(utcFor(9, 20))}, false, DatumAGL, nil)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}

	if !res.QuotaExhausted {
		t.Error("quotaOK=false 时 QuotaExhausted 必须为 true")
	}
	if res.Active {
		t.Error("配额耗尽时这条轨没取到任何数据，Active 应为 false")
	}

	for i := range rows {
		if got := res.Rows[i].NoDataReason; got != RoundQuotaDown {
			t.Errorf("第 %d 行应判 %s，得到 %s", i, RoundQuotaDown, got)
		}
	}

	if len(res.Rows) != len(rows) {
		t.Errorf("配额耗尽时 Rows 仍须与 A 轨等长，得到 %d/%d",
			len(res.Rows), len(rows))
	}

	if res.NextAvailable != nil {
		t.Error("Assemble 无从得知配额何时恢复，NextAvailable 必须留 nil")
	}
}

func TestNoSamplesIsKeyMissingNotOutOfHorizon(t *testing.T) {
	rows := rowsAt(localHour(9, 20), localHour(9, 21))

	res, err := Assemble("empty", testUTCOffset, 100,
		rows, nil, true, DatumAGL, nil)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}
	if res.Active {
		t.Error("零样本时 Active 应为 false")
	}
	for i := range rows {
		if got := res.Rows[i].NoDataReason; got != KeyMissing {
			t.Errorf("第 %d 行零样本应判 %s（可重试），得到 %s",
				i, KeyMissing, got)
		}
	}
}

func TestAssembleRowsCarryCapabilityFlag(t *testing.T) {
	rows := rowsAt(localHour(9, 20), localHour(9, 21), localHour(12, 20))
	samples := []HourInput{sampleAt(utcFor(9, 20))}

	res, err := Assemble("caps", testUTCOffset, 100,
		rows, samples, true, DatumAGL, nil)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}
	for i, v := range res.Rows {
		if !v.SeaBelowUnknown {
			t.Errorf("第 %d 行（%s）漏了 SeaBelowUnknown", i, v.NoDataReason)
		}
	}
	if res.Capabilities.HasCloudTopData {
		t.Error("B 轨没有云顶字段，HasCloudTopData 必须为 false")
	}
	if !res.Capabilities.SeaBelowUnknown {
		t.Error("B 轨云海一律不可判，SeaBelowUnknown 必须为 true")
	}
}

func TestAssembleRefusesNonAGLDatum(t *testing.T) {
	for _, datum := range []string{"msl", "MSL", "", "agl_msl"} {
		res, err := Assemble("guard", testUTCOffset, 1382.6,
			rowsAt(localHour(9, 20)), []HourInput{sampleAt(utcFor(9, 20))},
			true, datum, nil)

		if err == nil {
			t.Errorf("datum=%q 必须报错，却放行了", datum)
			continue
		}
		if !errors.Is(err, ErrDatumNotAGL) {
			t.Errorf("datum=%q 应返回 ErrDatumNotAGL，得到 %v", datum, err)
		}
		if res != nil {
			t.Errorf("datum=%q 报错时不得返回半成品结果", datum)
		}
	}
}

func TestAssembleCarriesDeltaHAndFidelity(t *testing.T) {
	const siteAlt = 1382.6

	s := hourFor(t, 356, num(800))
	s.TimeUTC = utcFor(9, 20)

	res, err := Assemble("kuocangshan", testUTCOffset, siteAlt,
		rowsAt(localHour(9, 20)), []HourInput{s}, true, DatumAGL, nil)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}

	v := res.Rows[0]
	if !v.DeltaH.Valid {
		t.Fatalf("有效样本应算出 ΔH，实际缺测（Note：%s）", v.Note)
	}
	if got := v.DeltaH.V; got > -1026 || got < -1027 {
		t.Errorf("ΔH=%.1f，期望约 −1026.6（356 − 1382.6）", got)
	}

	if v.TerrainFidelity != TerrainFlattened {
		t.Errorf("ΔH≈−1026.6 应判 %s，得到 %s",
			TerrainFlattened, v.TerrainFidelity)
	}
}

func TestReusedThresholdsMatchRateChain(t *testing.T) {
	res, err := Assemble("th", testUTCOffset, 100,
		rowsAt(localHour(9, 20)), []HourInput{sampleAt(utcFor(9, 20))},
		true, DatumAGL, nil)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}

	if len(res.ThresholdsReused) == 0 {
		t.Fatal("B 轨确实复用了 A 轨的雾/能见度阈值，清单不该为空")
	}

	seen := map[string]bool{}
	for _, name := range res.ThresholdsReused {
		if seen[name] {
			t.Errorf("复用清单里 %q 重复登记", name)
		}
		seen[name] = true
	}

	for _, name := range res.ThresholdsUnavailable {
		if seen[name] {
			t.Errorf("%q 同时出现在「已复用」与「不可用」两份清单里，"+
				"两者互斥，必有一边分类错误", name)
		}
	}

	res.ThresholdsReused[0] = "TAMPERED"
	res2, _ := Assemble("th2", testUTCOffset, 100,
		rowsAt(localHour(9, 20)), []HourInput{sampleAt(utcFor(9, 20))},
		true, DatumAGL, nil)
	if res2.ThresholdsReused[0] == "TAMPERED" {
		t.Error("阈值清单被调用方改动后污染了下一次结果，必须返回副本")
	}
}

func TestNoDataCountCountsOnlyNoData(t *testing.T) {

	rows := rowsAt(localHour(9, 20), localHour(9, 21), localHour(9, 22),
		localHour(12, 20))
	samples := []HourInput{
		sampleAt(utcFor(9, 20)),
		sampleAt(utcFor(9, 22)),
	}

	res, err := Assemble("count", testUTCOffset, 100,
		rows, samples, true, DatumAGL, nil)
	if err != nil {
		t.Fatalf("不该报错：%v", err)
	}

	if got := res.NoDataCount(); got != 2 {
		t.Errorf("NoDataCount=%d，期望 2（21 时缺样本 + 8/12 超窗）", got)
	}
	if got := res.CountByReason(KeyMissing); got != 1 {
		t.Errorf("CountByReason(KeyMissing)=%d，期望 1——"+
			"21 时落在样本跨度之内，属于可重试的空洞，不是超窗", got)
	}
	if got := res.CountByReason(OutOfHorizon); got != 1 {
		t.Errorf("CountByReason(OutOfHorizon)=%d，期望 1", got)
	}
	if got := res.CountByReason(NoDataNone); got != 2 {
		t.Errorf("CountByReason(NoDataNone)=%d，期望 2（两行正常）", got)
	}
}
