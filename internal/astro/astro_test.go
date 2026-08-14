package astro

import (
	"math"
	"testing"
	"time"
)

func utc(t *testing.T, iso string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		t.Fatalf("测试数据 %q 不是合法 RFC3339：%v", iso, err)
	}
	return v.UTC()
}

func closeTo(t *testing.T, label string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) {
		t.Fatalf("%s = NaN，期望 %.6f", label, want)
	}
	if diff := math.Abs(got - want); diff > tol {
		t.Errorf("%s = %.6f，期望 %.6f（容差 %.6f，实际偏差 %.6f）", label, got, want, tol, diff)
	}
}

func angleDiff(a, b float64) float64 {
	d := pyMod(a-b, 360.0)
	if d > 180 {
		d = 360 - d
	}
	return d
}

func TestJulianDayKnownEpochs(t *testing.T) {
	cases := []struct {
		name string
		iso  string
		want float64
	}{

		{"J2000.0 历元", "2000-01-01T12:00:00Z", 2451545.0},

		{"MJD 0 起点", "1858-11-17T00:00:00Z", 2400000.5},

		{"J2000 当天零时", "2000-01-01T00:00:00Z", 2451544.5},

		{"J2000 当天 18:00", "2000-01-01T18:00:00Z", 2451545.25},

		{"2000-02-29 闰日", "2000-02-29T00:00:00Z", 2451603.5},
		{"2001-01-01", "2001-01-01T00:00:00Z", 2451910.5},

		{"1900-01-01（非闰年）", "1900-01-01T00:00:00Z", 2415020.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := JulianDay(utc(t, c.iso))

			closeTo(t, "JulianDay", got, c.want, 1e-6)
		})
	}
}

func TestJulianDayIsStrictlyMonotonic(t *testing.T) {
	start := utc(t, "1999-12-25T00:00:00Z")
	prev := math.Inf(-1)
	for i := 0; i < 24*30; i++ {
		cur := JulianDay(start.Add(time.Duration(i) * time.Hour))
		if cur <= prev {
			t.Fatalf("第 %d 小时儒略日倒退：%.6f -> %.6f", i, prev, cur)
		}
		prev = cur
	}
}

func TestGMSTAtJ2000(t *testing.T) {
	got := GMSTDeg(2451545.0)

	closeTo(t, "GMSTDeg(J2000)", got, 280.46061837, 1e-6)
}

func TestGMSTAlwaysInRange(t *testing.T) {
	start := utc(t, "1900-01-01T00:00:00Z")
	for i := 0; i < 200; i++ {
		jd := JulianDay(start.AddDate(0, 0, i*250))
		g := GMSTDeg(jd)
		if g < 0 || g >= 360 {
			t.Fatalf("GMSTDeg(jd=%.3f) = %.6f，超出 [0,360)", jd, g)
		}
	}
}

func TestPyModMatchesPythonSemantics(t *testing.T) {
	cases := []struct{ x, m, want float64 }{
		{370, 360, 10},
		{-10, 360, 350},
		{-370, 360, 350},
		{360, 360, 0},
		{0, 360, 0},
		{-0.5, 29.530588853, 29.030588853},
	}
	for _, c := range cases {
		got := pyMod(c.x, c.m)
		closeTo(t, "pyMod", got, c.want, 1e-9)
	}
}

func TestSunRADecAtEquinoxesAndSolstices(t *testing.T) {
	const (
		posTol = 0.02
		obliq  = 23.4366
	)
	cases := []struct {
		name    string
		iso     string
		wantRA  float64
		wantDec float64
		decTol  float64
	}{
		{"春分 2024-03-20 03:06 UTC", "2024-03-20T03:06:00Z", 0, 0, posTol},
		{"夏至 2024-06-20 20:51 UTC", "2024-06-20T20:51:00Z", 90, obliq, posTol},
		{"秋分 2024-09-22 12:44 UTC", "2024-09-22T12:44:00Z", 180, 0, posTol},
		{"冬至 2024-12-21 09:21 UTC", "2024-12-21T09:21:00Z", 270, -obliq, posTol},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			jd := JulianDay(utc(t, c.iso))
			ra, dec := SunRADec(jd)

			if d := angleDiff(ra, c.wantRA); d > posTol {
				t.Errorf("太阳赤经 = %.6f°，期望 %.1f°（圆周偏差 %.6f° > %.6f°）",
					ra, c.wantRA, d, posTol)
			}
			closeTo(t, "太阳赤纬", dec, c.wantDec, c.decTol)
		})
	}
}

func TestSunDeclinationStaysWithinObliquity(t *testing.T) {
	const obliq = 23.4400
	start := utc(t, "2024-01-01T00:00:00Z")
	var minDec, maxDec = math.Inf(1), math.Inf(-1)
	for i := 0; i < 366; i++ {
		_, dec := SunRADec(JulianDay(start.AddDate(0, 0, i)))
		if math.IsNaN(dec) {
			t.Fatalf("第 %d 天太阳赤纬为 NaN", i)
		}
		minDec = math.Min(minDec, dec)
		maxDec = math.Max(maxDec, dec)
	}
	if maxDec > obliq || minDec < -obliq {
		t.Errorf("太阳赤纬全年范围 [%.4f, %.4f]，越过黄赤交角 ±%.4f", minDec, maxDec, obliq)
	}

	if maxDec < obliq-0.1 || minDec > -(obliq-0.1) {
		t.Errorf("太阳赤纬全年范围 [%.4f, %.4f] 没能覆盖二至，公式可能退化", minDec, maxDec)
	}
}

func TestAltitudeAtPoleEqualsDeclination(t *testing.T) {
	jd := JulianDay(utc(t, "2024-06-20T20:51:00Z"))
	for _, dec := range []float64{-89, -45, -23.44, 0, 23.44, 45, 89} {
		for _, ra := range []float64{0, 90, 180, 270} {
			for _, lon := range []float64{-180, 0, 116.4, 180} {
				got := AltitudeDeg(jd, 90, lon, ra, dec)
				closeTo(t, "北极高度角", got, dec, 1e-9)
			}
		}
	}
}

func TestAltitudeAtEquatorSolarNoonIsZenith(t *testing.T) {
	jd := JulianDay(utc(t, "2024-03-20T03:06:00Z"))
	ra, dec := SunRADec(jd)

	lon := ra - GMSTDeg(jd)
	got := AltitudeDeg(jd, 0, lon, ra, dec)
	closeTo(t, "赤道春分正午太阳高度角", got, 90, 0.02)
}

func TestAltitudeNeverEscapesRange(t *testing.T) {
	jd := JulianDay(utc(t, "2026-08-13T00:00:00Z"))
	for _, lat := range []float64{-90, -89.9999, -45, 0, 39.9, 89.9999, 90} {
		for _, lon := range []float64{-180, -90, 0, 116.4, 180} {
			for _, dec := range []float64{-90, -23.44, 0, 23.44, 90} {
				for _, ra := range []float64{0, 123.4, 359.9} {
					got := AltitudeDeg(jd, lat, lon, ra, dec)
					if math.IsNaN(got) {
						t.Fatalf("AltitudeDeg(lat=%v,lon=%v,ra=%v,dec=%v) = NaN", lat, lon, ra, dec)
					}
					if got < -90.000001 || got > 90.000001 {
						t.Fatalf("AltitudeDeg(lat=%v,lon=%v,ra=%v,dec=%v) = %.6f，超出 ±90",
							lat, lon, ra, dec, got)
					}
				}
			}
		}
	}
}

func TestMoonPhaseAtKnownSyzygies(t *testing.T) {
	cases := []struct {
		name      string
		iso       string
		wantAge   float64
		ageTol    float64
		wantIllum float64
		illumTol  float64
		wantName  string
	}{
		{"新月 2024-01-11 11:57 UTC", "2024-01-11T11:57:00Z", 0.0, 0.6, 0.0, 0.02, "新月"},
		{"上弦 2024-01-18 03:53 UTC", "2024-01-18T03:53:00Z", 7.38, 0.6, 0.5, 0.06, "上弦月"},
		{"满月 2024-01-25 17:54 UTC", "2024-01-25T17:54:00Z", 14.77, 0.6, 1.0, 0.02, "满月"},
		{"下弦 2024-02-02 23:18 UTC", "2024-02-02T23:18:00Z", 22.15, 0.7, 0.5, 0.08, "下弦月"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			jd := JulianDay(utc(t, c.iso))
			age, illum, name := MoonPhase(jd)
			closeTo(t, "月龄", age, c.wantAge, c.ageTol)
			closeTo(t, "照度", illum, c.wantIllum, c.illumTol)
			if name != c.wantName {
				t.Errorf("月相名 = %q，期望 %q（月龄 %.4f）", name, c.wantName, age)
			}
		})
	}
}

func TestMoonPhaseWrapsToNewMoonAtLunationEnd(t *testing.T) {
	jd := JulianDay(utc(t, "2026-08-12T17:37:00Z"))
	age, illum, name := MoonPhase(jd)
	if age < 27.68 {
		t.Fatalf("测试前提失效：月龄 %.4f 未落在默认分支区间 [27.68, 29.53)", age)
	}
	if name != "新月" {
		t.Errorf("朔前月龄 %.4f 的月相名 = %q，期望回落到 %q", age, name, "新月")
	}
	if illum > 0.02 {
		t.Errorf("朔时照度 = %.4f，期望接近 0", illum)
	}
}

func TestMoonPhaseIllumIsBoundedAndPeriodic(t *testing.T) {
	start := utc(t, "2024-01-01T00:00:00Z")
	for i := 0; i < 24*60; i++ {
		jd := JulianDay(start.Add(time.Duration(i) * time.Hour))
		age, illum, name := MoonPhase(jd)
		if age < 0 || age >= SynodicMonth {
			t.Fatalf("第 %d 小时月龄 %.6f 超出 [0, %.6f)", i, age, SynodicMonth)
		}
		if illum < 0 || illum > 1 {
			t.Fatalf("第 %d 小时照度 %.6f 超出 [0,1]", i, illum)
		}
		if name == "" {
			t.Fatalf("第 %d 小时月相名为空（月龄 %.4f）", i, age)
		}
	}
}

func TestMoonSunSyzygyGeometry(t *testing.T) {
	cases := []struct {
		name    string
		iso     string
		wantSep float64
		tol     float64
	}{
		{"朔：日月合", "2024-01-11T11:57:00Z", 0, 8},
		{"望：日月冲", "2024-01-25T17:54:00Z", 180, 8},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			jd := JulianDay(utc(t, c.iso))
			sunRA, _ := SunRADec(jd)
			moonRA, _ := MoonRADec(jd)
			sep := angleDiff(moonRA, sunRA)
			if math.Abs(sep-c.wantSep) > c.tol {
				t.Errorf("日月赤经差 = %.4f°，期望约 %.0f°（容差 %.0f°）", sep, c.wantSep, c.tol)
			}
		})
	}
}

func TestMoonRADecStaysNearEcliptic(t *testing.T) {
	const maxDec = 28.7
	start := utc(t, "2024-01-01T00:00:00Z")
	for i := 0; i < 24*30; i++ {
		jd := JulianDay(start.Add(time.Duration(i) * time.Hour))
		ra, dec := MoonRADec(jd)
		if ra < 0 || ra >= 360 {
			t.Fatalf("第 %d 小时月亮赤经 %.6f 超出 [0,360)", i, ra)
		}
		if math.Abs(dec) > maxDec {
			t.Fatalf("第 %d 小时月亮赤纬 %.6f 越过 ±%.1f", i, dec, maxDec)
		}
	}
}

func TestComputeUsesWallClockSemantics(t *testing.T) {
	const lat, lon = 40.5, 116.4

	beijing := Compute(time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC), 8*3600, lat, lon, -18)
	asUTC := Compute(time.Date(2026, 8, 12, 18, 0, 0, 0, time.UTC), 0, lat, lon, -18)
	if beijing != asUTC {
		t.Errorf("同一瞬间的两种表达结果不一致：\n  北京读数 %+v\n  UTC 读数 %+v", beijing, asUTC)
	}

	shanghai := time.FixedZone("CST", 8*3600)
	tagged := Compute(time.Date(2026, 8, 13, 2, 0, 0, 0, shanghai), 8*3600, lat, lon, -18)
	if tagged != beijing {
		t.Errorf("localDT 携带时区改变了结果：\n  带时区 %+v\n  不带   %+v", tagged, beijing)
	}
}

func TestComputeAstroDarkIsInclusiveThreshold(t *testing.T) {
	localDT := time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC)

	probe := Compute(localDT, 8*3600, 40.5, 116.4, -180)
	alt := probe.SunAlt
	if probe.AstroDark {
		t.Fatalf("测试前提失效：阈值 -180° 不该判为暗夜（sunAlt=%.6f）", alt)
	}

	if got := Compute(localDT, 8*3600, 40.5, 116.4, alt); !got.AstroDark {
		t.Errorf("sunAlt(%.6f) == darkSunAlt 时应判为暗夜（闭区间），实际未判", alt)
	}
	justBelow := math.Nextafter(alt, math.Inf(-1))
	if got := Compute(localDT, 8*3600, 40.5, 116.4, justBelow); got.AstroDark {
		t.Errorf("darkSunAlt(%.17g) 略低于 sunAlt(%.17g) 时不应判为暗夜", justBelow, alt)
	}
}

func TestComputePerseids2026Night(t *testing.T) {
	const (
		lat = 40.5573
		lon = 115.8407
	)
	got := Compute(time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC), 8*3600, lat, lon, -18.0)

	if !got.AstroDark {
		t.Errorf("凌晨 2 点应为天文暗夜，实际 sunAlt=%.4f", got.SunAlt)
	}
	if got.SunAlt > -18 {
		t.Errorf("凌晨 2 点太阳高度角 = %.4f，应低于 -18°", got.SunAlt)
	}

	if got.MoonIllum > 0.05 {
		t.Errorf("朔后一夜月亮照度 = %.4f，期望接近 0", got.MoonIllum)
	}
	if got.MoonPhaseName != "新月" {
		t.Errorf("月相名 = %q，期望 %q", got.MoonPhaseName, "新月")
	}

	if math.IsNaN(got.GCAlt) || got.GCAlt < -90 || got.GCAlt > 90 {
		t.Errorf("银心高度角 = %.4f，超出合理范围", got.GCAlt)
	}
}

func TestComputeNoonIsNotAstroDark(t *testing.T) {
	got := Compute(time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC), 8*3600, 40.5, 116.4, -18.0)
	if got.AstroDark {
		t.Errorf("正午被判为天文暗夜（sunAlt=%.4f）", got.SunAlt)
	}
	if got.SunAlt <= 0 {
		t.Errorf("北纬 40° 夏季正午太阳高度角 = %.4f，应显著高于地平线", got.SunAlt)
	}
}

func TestComputeGCUsesCatalogPosition(t *testing.T) {
	localDT := time.Date(2026, 8, 13, 2, 0, 0, 0, time.UTC)
	got := Compute(localDT, 8*3600, 40.5573, 115.8407, -18.0)
	jd := JulianDay(localDT.Add(-8 * time.Hour))
	want := AltitudeDeg(jd, 40.5573, 115.8407, GCRADeg, GCDecDeg)
	closeTo(t, "银心高度角", got.GCAlt, want, 1e-12)

	if math.Abs(got.GCAlt-got.SunAlt) < 1e-9 {
		t.Error("银心高度角与太阳完全相同，GCRADeg/GCDecDeg 可能被误用")
	}
}
