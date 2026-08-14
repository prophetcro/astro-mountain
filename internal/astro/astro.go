// Package astro 提供纯 Go 的天文近似算法。
//
// 覆盖：太阳/月亮赤经赤纬、地平高度角、月相与月龄、银心（Sgr A*）高度、
// 天文暗夜判定。全部为低精度星历近似，不下载任何外部星历文件，
// 足以支撑『某天体是否在地平线上 / 是否天文暗夜 / 银心多高』这类判定。
package astro

import (
	"math"
	"time"
)

const (
	// SynodicMonth 朔望月长度（天），用于月相与月龄计算。
	SynodicMonth = 29.530588853

	// NewMoonJD 参考新月时刻的儒略日（2000-01-06 18:14 UTC）。
	NewMoonJD = 2451550.09766

	// GCRADeg 银心 Sgr A* 的赤经（度）。
	GCRADeg = 266.4168

	// GCDecDeg 银心 Sgr A* 的赤纬（度）。
	GCDecDeg = -29.0078
)

// pyMod 复刻 Python 的 % 语义：结果符号跟随除数（对正除数恒为非负）。
// Go 的 math.Mod 结果符号跟随被除数，直接在负角上用会与 Python 产生分歧，故修正之。
func pyMod(x, m float64) float64 {
	r := math.Mod(x, m)
	if r != 0 && (r < 0) != (m < 0) {
		r += m
	}
	return r
}

// JulianDay 公历 UTC 时间转儒略日（含小数）。
func JulianDay(dtUTC time.Time) float64 {
	year := dtUTC.Year()
	month := int(dtUTC.Month())
	day := float64(dtUTC.Day()) +
		float64(dtUTC.Hour())/24.0 +
		float64(dtUTC.Minute())/1440.0 +
		float64(dtUTC.Second())/86400.0
	if month <= 2 {
		year--
		month += 12
	}
	a := year / 100
	b := 2 - a + a/4
	return float64(int(365.25*float64(year+4716))) +
		float64(int(30.6001*float64(month+1))) +
		day + float64(b) - 1524.5
}

// GMSTDeg 格林尼治平恒星时（度）。
func GMSTDeg(jd float64) float64 {
	d := jd - 2451545.0
	t := d / 36525.0
	gmst := 280.46061837 + 360.98564736629*d +
		0.000387933*t*t - t*t*t/38710000.0
	return pyMod(gmst, 360.0)
}

// ObliquityRad 黄赤交角（弧度）。
func ObliquityRad(jd float64) float64 {
	return degToRad(23.439291 - 0.0000004*(jd-2451545.0))
}

// SunRADec 太阳赤经赤纬（度），低精度公式（Astronomical Almanac）。
func SunRADec(jd float64) (raDeg, decDeg float64) {
	d := jd - 2451545.0
	meanLon := pyMod(280.460+0.9856474*d, 360.0)
	meanAnom := degToRad(pyMod(357.528+0.9856003*d, 360.0))
	eclLon := degToRad(meanLon + 1.915*math.Sin(meanAnom) + 0.020*math.Sin(2*meanAnom))
	eps := ObliquityRad(jd)
	ra := pyMod(radToDeg(math.Atan2(math.Cos(eps)*math.Sin(eclLon), math.Cos(eclLon))), 360.0)
	dec := radToDeg(math.Asin(math.Sin(eps) * math.Sin(eclLon)))
	return ra, dec
}

// AltitudeDeg 给定天体赤经赤纬，算其在 (lat, lon) 的地平高度角（度），未做大气折射修正。
func AltitudeDeg(jd, lat, lon, raDeg, decDeg float64) float64 {
	lst := pyMod(GMSTDeg(jd)+lon, 360.0)
	hourAngle := degToRad(pyMod(lst-raDeg, 360.0))
	latR := degToRad(lat)
	decR := degToRad(decDeg)
	sinAlt := math.Sin(decR)*math.Sin(latR) +
		math.Cos(decR)*math.Cos(latR)*math.Cos(hourAngle)
	return radToDeg(math.Asin(clamp(sinAlt, -1.0, 1.0)))
}

func degToRad(d float64) float64 { return d * math.Pi / 180.0 }
func radToDeg(r float64) float64 { return r * 180.0 / math.Pi }

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Info 某时刻、某站点的天文状态快照。
type Info struct {
	SunAlt        float64 // 太阳地平高度角（度）
	MoonAlt       float64 // 月亮地平高度角（度）
	MoonIllum     float64 // 月亮 illumination 比例 [0,1]
	MoonPhaseName string  // 月相中文名
	GCAlt         float64 // 银心 Sgr A* 地平高度角（度）
	AstroDark     bool    // 太阳高度 ≤ darkSunAlt 即天文暗夜
}

// Compute 计算给定本地时间、UTC 偏移、站点坐标下的天文状态。
//
// darkSunAlt 为『天文暗夜』的太阳高度阈值（通常为负值，如 -12° 或 -18°）。
// utcOffsetSec 为该站点的 UTC 偏移秒数，用于把本地时间换算回 UTC 再求儒略日。
func Compute(localDT time.Time, utcOffsetSec int, lat, lon, darkSunAlt float64) Info {
	dtUTC := localDT.Add(-time.Duration(utcOffsetSec) * time.Second)
	jd := JulianDay(dtUTC)

	sunRA, sunDec := SunRADec(jd)
	moonRA, moonDec := MoonRADec(jd)
	_, illum, phaseName := MoonPhase(jd)
	sunAlt := AltitudeDeg(jd, lat, lon, sunRA, sunDec)

	return Info{
		SunAlt:        sunAlt,
		MoonAlt:       AltitudeDeg(jd, lat, lon, moonRA, moonDec),
		MoonIllum:     illum,
		MoonPhaseName: phaseName,
		GCAlt:         AltitudeDeg(jd, lat, lon, GCRADeg, GCDecDeg),
		AstroDark:     sunAlt <= darkSunAlt,
	}
}
