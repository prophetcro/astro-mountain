package tomorrow

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/prophetcro/astro-mountain/internal/model"
)

const coldLakeAlt = 2790.0

func TestConvertHeightColdLakeMeasured(t *testing.T) {

	agl, msl := convertHeight(model.Num(7.98), UnitKilometer, coldLakeAlt, DatumAGL)
	mustOpt(t, agl, 7980, "AGL 米")
	mustOpt(t, msl, 7980+coldLakeAlt, "MSL 米")
}

func TestConvertHeightMissingStaysMissing(t *testing.T) {
	agl, msl := convertHeight(model.Missing(), UnitKilometer, coldLakeAlt, DatumAGL)
	mustMissing(t, agl, "缺测输入的 AGL")
	mustMissing(t, msl, "缺测输入的 MSL")

	if agl.Or(-1) != -1 || msl.Or(-1) != -1 {
		t.Error("缺测被换算出了具体数值")
	}
}

func TestConvertHeightZeroIsRealValue(t *testing.T) {
	agl, msl := convertHeight(model.Num(0), UnitKilometer, coldLakeAlt, DatumAGL)
	mustOpt(t, agl, 0, "云底贴地的 AGL")

	mustOpt(t, msl, coldLakeAlt, "云底贴地的 MSL")
}

func TestConvertHeightSmallestMeasuredProvesAGL(t *testing.T) {
	agl, msl := convertHeight(model.Num(0.1), UnitKilometer, coldLakeAlt, DatumAGL)
	mustOpt(t, agl, 100, "最小实测值的 AGL")
	mustOpt(t, msl, 2890, "最小实测值的 MSL")
	if msl.Must() < coldLakeAlt {
		t.Error("AGL 基准下云底 MSL 不可能低于机位海拔")
	}
}

func TestConvertHeightMSLDatumBackfillsAGL(t *testing.T) {
	agl, msl := convertHeight(model.Num(7.98), UnitKilometer, coldLakeAlt, DatumMSL)
	mustOpt(t, msl, 7980, "MSL 基准下的 MSL")
	mustOpt(t, agl, 7980-coldLakeAlt, "MSL 基准下反推的 AGL")
}

func TestConvertHeightExtremes(t *testing.T) {
	agl, msl := convertHeight(model.Num(1e6), UnitKilometer, coldLakeAlt, DatumAGL)
	mustOpt(t, agl, 1e9, "极大值不裁剪")
	if !msl.Valid {
		t.Error("极大值的 MSL 也应有效")
	}

	nan, nanMSL := convertHeight(model.Num(math.NaN()), UnitKilometer, coldLakeAlt, DatumAGL)
	mustMissing(t, nan, "NaN 的 AGL")
	mustMissing(t, nanMSL, "NaN 的 MSL")

	inf, infMSL := convertHeight(model.Num(math.Inf(1)), UnitKilometer, coldLakeAlt, DatumAGL)
	mustMissing(t, inf, "Inf 的 AGL")
	mustMissing(t, infMSL, "Inf 的 MSL")
}

func TestConvertHeightUnresolvedAutoDegradesToMissing(t *testing.T) {
	agl, msl := convertHeight(model.Num(7.98), UnitAuto, coldLakeAlt, DatumAGL)
	mustMissing(t, agl, "auto 单位的 AGL")
	mustMissing(t, msl, "auto 单位的 MSL")
}

func TestConvertHeightAllUnits(t *testing.T) {
	cases := []struct {
		unit Unit
		raw  float64
		want float64
	}{
		{UnitKilometer, 7.98, 7980},
		{UnitMeter, 7980, 7980},
		{UnitFeet, 1000, 304.8},
		{UnitFeet, 0, 0},
		{UnitMeter, -50, -50},
	}
	for _, c := range cases {
		agl, _ := convertHeight(model.Num(c.raw), c.unit, 0, DatumAGL)
		mustOpt(t, agl, c.want, string(c.unit))
	}
}

func TestCheckUnitSanityAcceptsMeasuredMagnitude(t *testing.T) {

	samples := []float64{0.1, 7.98, 8.7, 2.4}
	if err := CheckUnitSanity(UnitKilometer, samples); err != nil {
		t.Errorf("实测量级与 km 自洽，不该报警：%v", err)
	}
}

func TestCheckUnitSanityAlarmsOnMagnitudeShift(t *testing.T) {
	samples := []float64{100, 7980, 8700}
	err := CheckUnitSanity(UnitKilometer, samples)
	if err == nil {
		t.Fatal("量级从 km 跳到 m 时哨兵必须报警")
	}
	if !errors.Is(err, ErrUnitMismatch) {
		t.Errorf("错误应可用 errors.Is(ErrUnitMismatch) 识别，实际 %v", err)
	}

	msg := err.Error()
	for _, kw := range []string{"km", "配置", "config.json", "tomorrow_cloud_base_unit"} {
		if !strings.Contains(msg, kw) {
			t.Errorf("报警文案缺少关键信息 %q：%s", kw, msg)
		}
	}
}

func TestCheckUnitSanityAlarmsBothDirections(t *testing.T) {
	if err := CheckUnitSanity(UnitMeter, []float64{0.1, 7.98}); err == nil {
		t.Error("配置为 m 但量级像 km 时应报警")
	}
}

func TestCheckUnitSanityStaysQuietWhenUndecidable(t *testing.T) {
	if err := CheckUnitSanity(UnitAuto, []float64{7.98}); err != nil {
		t.Errorf("auto 模式不该走哨兵：%v", err)
	}
	if err := CheckUnitSanity(UnitKilometer, nil); err != nil {
		t.Errorf("无样本时不该报警：%v", err)
	}
	if err := CheckUnitSanity(UnitKilometer, []float64{math.NaN(), math.Inf(1)}); err != nil {
		t.Errorf("全非有限数时不该报警：%v", err)
	}

	if err := CheckUnitSanity(UnitKilometer, []float64{1e9}); err != nil {
		t.Errorf("量级离谱到判不出单位时不该报假警：%v", err)
	}
}

func TestCheckUnitSanityDoesNotClaimMeterVersusFeet(t *testing.T) {
	samples := []float64{1000, 5000}
	if err := CheckUnitSanity(UnitMeter, samples); err != nil {
		t.Errorf("m 配置 + 米级量级不该报警：%v", err)
	}
	if err := CheckUnitSanity(UnitFeet, samples); err != nil {
		t.Errorf("ft 与 m 启发式无法区分，不该报警：%v", err)
	}
}

func TestCheckUnitSanityBoundary(t *testing.T) {
	if err := CheckUnitSanity(UnitKilometer, []float64{kmUpperBound}); err != nil {
		t.Errorf("恰好 %g 仍算 km，不该报警：%v", kmUpperBound, err)
	}
	if err := CheckUnitSanity(UnitKilometer, []float64{kmUpperBound + 0.01}); err == nil {
		t.Errorf("刚过 %g 就该按米级看待并报警", kmUpperBound)
	}
}

func TestMaxAbsOf(t *testing.T) {
	cases := []struct {
		name    string
		samples []float64
		want    float64
	}{
		{"空", nil, 0},
		{"全非有限", []float64{math.NaN(), math.Inf(-1)}, 0},
		{"取绝对值", []float64{-8.7, 7.98}, 8.7},
		{"混合", []float64{math.NaN(), 0.1, -12}, 12},
		{"全零", []float64{0, 0}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := maxAbsOf(c.samples); got != c.want {
				t.Errorf("maxAbsOf(%v) = %g，期望 %g", c.samples, got, c.want)
			}
		})
	}
}
