package tomorrow

import (
	"errors"
	"math"
	"testing"
)

func TestParseUnit(t *testing.T) {
	cases := []struct {
		in      string
		want    Unit
		wantErr bool
	}{
		{"auto", UnitAuto, false},
		{"AUTO", UnitAuto, false},
		{"  m  ", UnitMeter, false},
		{"ft", UnitFeet, false},
		{"KM", UnitKilometer, false},

		{"", UnitAuto, false},
		{"meters", UnitAuto, true},
		{"米", UnitAuto, true},
	}
	for _, tc := range cases {
		got, err := ParseUnit(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseUnit(%q) err = %v，期望 wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseUnit(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestParseDatum(t *testing.T) {
	cases := []struct {
		in      string
		want    Datum
		wantErr bool
	}{
		{"agl", DatumAGL, false},
		{"MSL", DatumMSL, false},
		{" msl ", DatumMSL, false},
		{"", DatumAGL, false},
		{"ground", DatumAGL, true},
	}
	for _, tc := range cases {
		got, err := ParseDatum(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseDatum(%q) err = %v，期望 wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseDatum(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestToMeters(t *testing.T) {
	cases := []struct {
		v    float64
		u    Unit
		want float64
	}{
		{1500, UnitMeter, 1500},
		{1.5, UnitKilometer, 1500},
		{5000, UnitFeet, 1524},
		{0, UnitMeter, 0},
		{-200, UnitMeter, -200},
	}
	for _, tc := range cases {
		got, err := ToMeters(tc.v, tc.u)
		if err != nil {
			t.Errorf("ToMeters(%g, %q) 意外报错：%v", tc.v, tc.u, err)
			continue
		}
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("ToMeters(%g, %q) = %g，期望 %g", tc.v, tc.u, got, tc.want)
		}
	}
}

func TestToMetersRejectsAuto(t *testing.T) {
	if _, err := ToMeters(1500, UnitAuto); !errors.Is(err, ErrUnitUnresolved) {
		t.Fatalf("ToMeters(_, auto) err = %v，期望 ErrUnitUnresolved", err)
	}
}

func TestToMetersRejectsNonFinite(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := ToMeters(v, UnitMeter); err == nil {
			t.Errorf("ToMeters(%v, m) 应当报错", v)
		}
	}
}

func TestDetectUnit(t *testing.T) {
	cases := []struct {
		name    string
		samples []float64
		want    Unit
		wantErr bool
	}{
		{"千米量级", []float64{1.2, 1.5, 0.8}, UnitKilometer, false},
		{"米量级", []float64{800, 1500, 2300}, UnitMeter, false},
		{"恰好落在 km 上界", []float64{25}, UnitKilometer, false},
		{"刚过 km 上界即判米", []float64{25.1}, UnitMeter, false},
		{"恰好落在米上界", []float64{40000}, UnitMeter, false},
		{"超出米上界无法判定", []float64{40001}, UnitAuto, true},
		{"空样本无法判定", nil, UnitAuto, true},
		{"全是 NaN 视同空样本", []float64{math.NaN(), math.Inf(1)}, UnitAuto, true},
		{"混入 NaN 不影响判定", []float64{math.NaN(), 1.2}, UnitKilometer, false},
		{"取绝对值最大者", []float64{-1500, 2.0}, UnitMeter, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DetectUnit(tc.samples)
			if (err != nil) != tc.wantErr {
				t.Fatalf("DetectUnit(%v) err = %v，期望 wantErr=%v", tc.samples, err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrUnitUnknown) {
				t.Fatalf("DetectUnit 的错误应可用 errors.Is(ErrUnitUnknown) 判定，实际 %v", err)
			}
			if got != tc.want {
				t.Fatalf("DetectUnit(%v) = %q，期望 %q", tc.samples, got, tc.want)
			}
		})
	}
}

func TestDetectUnitCannotDistinguishMeterFromFeet(t *testing.T) {
	got, err := DetectUnit([]float64{5000})
	if err != nil {
		t.Fatalf("意外报错：%v", err)
	}
	if got != UnitMeter {
		t.Fatalf("DetectUnit([5000]) = %q，期望 m", got)
	}

	asFeet, _ := ToMeters(5000, UnitFeet)
	asMeter, _ := ToMeters(5000, UnitMeter)
	if math.Abs(asMeter-asFeet) < 1000 {
		t.Fatal("m 与 ft 的差异不该这么小，测试前提有误")
	}
}

func TestToMSLAndToAGL(t *testing.T) {
	const siteAlt = 1489.9

	if got := ToMSL(500, siteAlt, DatumAGL); math.Abs(got-1989.9) > 1e-9 {
		t.Errorf("ToMSL(500, %g, agl) = %g，期望 1989.9", siteAlt, got)
	}

	if got := ToMSL(1800, siteAlt, DatumMSL); got != 1800 {
		t.Errorf("ToMSL(1800, %g, msl) = %g，期望 1800", siteAlt, got)
	}

	roundTrip := ToAGL(ToMSL(500, siteAlt, DatumAGL), siteAlt)
	if math.Abs(roundTrip-500) > 1e-9 {
		t.Errorf("ToMSL→ToAGL 往返 = %g，期望回到 500", roundTrip)
	}
}
