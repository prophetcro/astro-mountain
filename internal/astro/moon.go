package astro

import "math"

// MoonRADec 月亮赤经赤纬（度），低精度月历公式（含黄纬的黄道坐标转赤道坐标）。
func MoonRADec(jd float64) (raDeg, decDeg float64) {
	d := jd - 2451545.0
	meanLon := 218.316 + 13.176396*d
	meanAnom := degToRad(134.963 + 13.064993*d)
	argLat := degToRad(93.272 + 13.229350*d)
	eclLon := degToRad(meanLon + 6.289*math.Sin(meanAnom))
	eclLat := degToRad(5.128 * math.Sin(argLat))
	eps := ObliquityRad(jd)

	cb, sb := math.Cos(eclLat), math.Sin(eclLat)
	cl, sl := math.Cos(eclLon), math.Sin(eclLon)
	x := cb * cl
	y := math.Cos(eps)*cb*sl - math.Sin(eps)*sb
	z := math.Sin(eps)*cb*sl + math.Cos(eps)*sb

	ra := pyMod(radToDeg(math.Atan2(y, x)), 360.0)
	dec := radToDeg(math.Asin(clamp(z, -1.0, 1.0)))
	return ra, dec
}

// phaseBound 月相区间上界（月龄，天）与对应中文名。
type phaseBound struct {
	limit float64
	label string
}

// phaseBounds 按月龄升序的月相划分，用于 MoonPhase 取最近区间。
var phaseBounds = []phaseBound{
	{1.85, "新月"},
	{5.54, "娥眉月"},
	{9.23, "上弦月"},
	{12.92, "盈凸月"},
	{16.61, "满月"},
	{20.30, "亏凸月"},
	{23.99, "下弦月"},
	{27.68, "残月"},
}

// MoonPhase 返回给定儒略日下的月龄（天）、月亮 illumination 比例与月相中文名。
func MoonPhase(jd float64) (age, illum float64, name string) {
	age = pyMod(jd-NewMoonJD, SynodicMonth)
	illum = (1.0 - math.Cos(2.0*math.Pi*age/SynodicMonth)) / 2.0
	name = "新月"
	for _, b := range phaseBounds {
		if age < b.limit {
			name = b.label
			break
		}
	}
	return age, illum, name
}
