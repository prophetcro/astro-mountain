package api

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/api/openmeteo"
)

// PressureLevels 是每次取数都请求的气压层（hPa），与 profile 包的反演层一致。
//
// 2026-09 升级到 11 层（在原 1000/975/950/925/900/850/800/700 之间插入 875/825/750）：
// Open-Meteo 只接受 25hPa 倍数的气压层，937/912/887 等返回全 null；
// 11 层下云海盲区由 8 层的 493m 降到 ≤260m（实测：20 站点 16 个盲区 >400m，
// 扩层后降至 1 个；中位盲区 494m → 248m）。
// 加 750hPa 是为了让 2000–3000m 高山站点（如冷湖镇 2790m、武功山 1918m）
// 也能被上下两层夹住，避免 850↔700 之间 1646m 的真空。
var PressureLevels = [...]int{1000, 975, 950, 925, 900, 875, 850, 825, 800, 750, 700}

// SurfaceRequired 是必须拿到的地面变量，缺任何一个都会让评级失去依据。
var SurfaceRequired = [...]string{
	"temperature_2m", "dew_point_2m", "relative_humidity_2m",
	"cloud_cover_low", "cloud_cover_mid", "cloud_cover_high",
	"wind_speed_10m", "weather_code",
}

// SurfaceOptional 是「有则更好」的地面变量。
// 部分模式并不提供它们，请求会整体报错，因此失败后会剔除这些变量重试。
var SurfaceOptional = [...]string{
	"visibility", "boundary_layer_height", "freezing_level_height",
	"precipitation",
}

// Open-Meteo 的可请求区间：向后最多 15 天预报，向前最多回溯 90 天。
const (
	ForecastMaxAheadDays = 15
	ForecastMaxPastDays  = 90
)

// BuildHourlyVars 拼出 hourly 参数需要的变量名列表：
// 必需地面变量 +（可选地面变量）+ 每个气压层的云量、位势高、相对湿度。
func BuildHourlyVars(includeOptional bool) []string {
	names := make([]string, 0, len(SurfaceRequired)+len(SurfaceOptional)+len(PressureLevels)*3)
	names = append(names, SurfaceRequired[:]...)
	if includeOptional {
		names = append(names, SurfaceOptional[:]...)
	}
	for _, p := range PressureLevels {
		names = append(names,
			fmt.Sprintf("cloud_cover_%dhPa", p),
			fmt.Sprintf("geopotential_height_%dhPa", p),
			fmt.Sprintf("relative_humidity_%dhPa", p),
		)
	}
	return names
}

// LevelVarNames 返回某气压层三个变量的 API 名称（云量、位势高、相对湿度）。
func LevelVarNames(pressure int) (cc, gh, rh string) {
	return fmt.Sprintf("cloud_cover_%dhPa", pressure),
		fmt.Sprintf("geopotential_height_%dhPa", pressure),
		fmt.Sprintf("relative_humidity_%dhPa", pressure)
}

// variableKey 是 FlatBuffers 响应里定位一条序列的三元组。
// FlatBuffers 不带变量名，只能靠「变量枚举 + 高度 + 气压层」反查回字符串名。
type variableKey struct {
	variable      openmeteo.Variable
	altitude      int16
	pressureLevel int16
}

// surfaceVarKeys 把地面变量名映射到 FlatBuffers 的定位三元组。
// 带高度后缀的变量（如 temperature_2m）靠 altitude 区分。
var surfaceVarKeys = map[string]variableKey{
	"temperature_2m":        {openmeteo.Variabletemperature, 2, 0},
	"dew_point_2m":          {openmeteo.Variabledew_point, 2, 0},
	"relative_humidity_2m":  {openmeteo.Variablerelative_humidity, 2, 0},
	"cloud_cover_low":       {openmeteo.Variablecloud_cover_low, 0, 0},
	"cloud_cover_mid":       {openmeteo.Variablecloud_cover_mid, 0, 0},
	"cloud_cover_high":      {openmeteo.Variablecloud_cover_high, 0, 0},
	"wind_speed_10m":        {openmeteo.Variablewind_speed, 10, 0},
	"visibility":            {openmeteo.Variablevisibility, 0, 0},
	"boundary_layer_height": {openmeteo.Variableboundary_layer_height, 0, 0},
	"freezing_level_height": {openmeteo.Variablefreezing_level_height, 0, 0},
	"weather_code":          {openmeteo.Variableweather_code, 0, 0},
	"precipitation":         {openmeteo.Variableprecipitation, 0, 0},
}

// levelVarPrefixes 是气压层变量名的前缀到变量枚举的映射，
// 前缀之后、"hPa" 之前的数字即气压层。
var levelVarPrefixes = map[string]openmeteo.Variable{
	"cloud_cover_":         openmeteo.Variablecloud_cover,
	"geopotential_height_": openmeteo.Variablegeopotential_height,
	"relative_humidity_":   openmeteo.Variablerelative_humidity,
}

// varKeyOf 把 API 变量名解析成 FlatBuffers 定位三元组。
// 无法识别的名字返回 false（例如层号不是正整数或超出 int16 范围），
// 调用方据此跳过该变量而不是崩掉。
func varKeyOf(name string) (variableKey, bool) {
	if k, ok := surfaceVarKeys[name]; ok {
		return k, true
	}
	if !strings.HasSuffix(name, "hPa") {
		return variableKey{}, false
	}

	for prefix, v := range levelVarPrefixes {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		digits := strings.TrimSuffix(strings.TrimPrefix(name, prefix), "hPa")
		p, err := strconv.Atoi(digits)
		if err != nil || p <= 0 || p > 32767 {
			return variableKey{}, false
		}
		return variableKey{variable: v, altitude: 0, pressureLevel: int16(p)}, true
	}
	return variableKey{}, false
}
