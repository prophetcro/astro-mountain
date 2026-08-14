package api

import (
	"encoding/binary"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/api/openmeteo"
)

const fixtureFile = "testdata/fb_star_mountain.bin"

func loadFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(fixtureFile)
	if err != nil {
		t.Fatalf("读取 fixture 失败：%v", err)
	}
	return data
}

func mustParseFixture(t *testing.T) *Response {
	t.Helper()
	resp, err := ParseResponse(loadFixture(t), BuildHourlyVars(true))
	if err != nil {
		t.Fatalf("解析 fixture 失败：%v", err)
	}
	return resp
}

func TestParseResponse_Metadata(t *testing.T) {
	resp := mustParseFixture(t)

	if got, want := resp.Latitude, 28.25; got != want {
		t.Errorf("Latitude = %v，期望 %v", got, want)
	}
	if got, want := resp.Longitude, 119.375; got != want {
		t.Errorf("Longitude = %v，期望 %v", got, want)
	}
	if got, want := resp.Elevation, 1000.0; got != want {
		t.Errorf("Elevation = %v，期望 %v", got, want)
	}
	if got, want := resp.UTCOffsetSeconds, 28800; got != want {
		t.Errorf("UTCOffsetSeconds = %d，期望 %d", got, want)
	}
	if got, want := resp.Timezone, "Asia/Shanghai"; got != want {
		t.Errorf("Timezone = %q，期望 %q", got, want)
	}
	if got, want := resp.Len(), 24; got != want {
		t.Fatalf("Len() = %d，期望 %d", got, want)
	}

	if got, want := resp.Times[0].Format(TimeLayout), "2026-08-12T00:00"; got != want {
		t.Errorf("Times[0] = %s，期望 %s", got, want)
	}
	if got, want := resp.Times[23].Format(TimeLayout), "2026-08-12T23:00"; got != want {
		t.Errorf("Times[23] = %s，期望 %s", got, want)
	}
	for i := 1; i < resp.Len(); i++ {
		if d := resp.Times[i].Sub(resp.Times[i-1]); d != time.Hour {
			t.Fatalf("Times[%d] 与前一时次间隔 %v，期望 1h", i, d)
		}
	}
}

func TestParseResponse_SurfaceValues(t *testing.T) {
	resp := mustParseFixture(t)

	cases := []struct {
		name string
		idx  int
		want float64
	}{
		{"temperature_2m", 0, 20.944000244140625},
		{"temperature_2m", 1, 20.993999481201172},
		{"dew_point_2m", 0, 20.112163543701172},
		{"relative_humidity_2m", 0, 95},
		{"cloud_cover_low", 0, 76},
		{"cloud_cover_mid", 0, 94},
		{"cloud_cover_high", 0, 97},
		{"wind_speed_10m", 1, 1.140175461769104},
		{"freezing_level_height", 0, 5860},
	}
	for _, c := range cases {
		got := resp.At(c.name, c.idx)
		if !got.Valid {
			t.Errorf("%s[%d] 被判为缺测，期望有效值 %v", c.name, c.idx, c.want)
			continue
		}
		if got.V != c.want {
			t.Errorf("%s[%d] = %v，期望 %v", c.name, c.idx, got.V, c.want)
		}
	}
}

func TestParseResponse_FullPrecision(t *testing.T) {
	resp := mustParseFixture(t)

	for _, name := range []string{"temperature_2m", "dew_point_2m"} {
		v := resp.At(name, 0)
		if !v.Valid {
			t.Fatalf("%s[0] 缺测，无法校验精度", name)
		}
		if v.V == math.Round(v.V*10)/10 {
			t.Errorf("%s[0] = %v 恰好是 0.1 的整数倍，疑似退回了 JSON 端点的截断值；"+
				"FlatBuffers 必须给出全精度 float32", name, v.V)
		}
	}

	temp := resp.At("temperature_2m", 0).V
	dew := resp.At("dew_point_2m", 0).V
	if got, want := temp-dew, 0.8318367004394531; got != want {
		t.Errorf("spread = %v，期望 %v（与 Python 逐位相同）", got, want)
	}
}

func TestParseResponse_LevelValues(t *testing.T) {
	resp := mustParseFixture(t)

	cases := []struct {
		name string
		idx  int
		want float64
	}{
		{"cloud_cover_1000hPa", 0, 32.7404670715332},
		{"geopotential_height_1000hPa", 0, -23},
		{"relative_humidity_1000hPa", 0, 95},
		{"geopotential_height_975hPa", 0, 196.1340789794922},
		{"cloud_cover_900hPa", 0, 20.727067947387695},
		{"relative_humidity_900hPa", 0, 88},
		{"relative_humidity_850hPa", 0, 97},
		{"geopotential_height_700hPa", 0, 3049},
		{"relative_humidity_700hPa", 2, 97.77777862548828},
	}
	for _, c := range cases {
		got := resp.At(c.name, c.idx)
		if !got.Valid {
			t.Errorf("%s[%d] 被判为缺测，期望有效值 %v", c.name, c.idx, c.want)
			continue
		}
		if got.V != c.want {
			t.Errorf("%s[%d] = %v，期望 %v", c.name, c.idx, got.V, c.want)
		}
	}

	levels := resp.LevelValues(0)
	if len(levels) != len(PressureLevels) {
		t.Fatalf("LevelValues 返回 %d 层，期望 %d 层", len(levels), len(PressureLevels))
	}
	for _, p := range PressureLevels {
		lv := levels[p]
		if !lv.CC.Valid || !lv.GH.Valid || !lv.RH.Valid {
			t.Errorf("%dhPa 层有缺测：cc=%v gh=%v rh=%v", p, lv.CC, lv.GH, lv.RH)
		}
	}

	surf, lvl := resp.Series["relative_humidity_2m"], resp.Series["relative_humidity_1000hPa"]
	if len(surf) == 0 || len(lvl) == 0 {
		t.Fatal("地表或气压层相对湿度序列缺失")
	}
	if &surf[0] == &lvl[0] {
		t.Error("relative_humidity_2m 与 relative_humidity_1000hPa 指向同一份数据（串号）")
	}
}

func TestParseResponse_NaNBecomesMissing(t *testing.T) {
	resp := mustParseFixture(t)

	for _, name := range []string{"visibility", "boundary_layer_height"} {
		series, ok := resp.Series[name]
		if !ok {
			t.Fatalf("%s 未出现在 Series 里，无法验证 NaN 语义", name)
		}
		if len(series) == 0 {
			t.Fatalf("%s 序列为空", name)
		}
		for i, v := range series {
			if v.Valid {
				t.Fatalf("%s[%d] 被判为有效值 %v，期望缺测（NaN 必须转成 Missing）", name, i, v.V)
			}
			if v.V != 0 {
				t.Errorf("%s[%d] 缺测时底层值应为 0，实际 %v", name, i, v.V)
			}

			if got := v.Or(-1); got != -1 {
				t.Errorf("%s[%d].Or(-1) = %v，缺测值不应回落到 0", name, i, got)
			}
		}
	}

	s := resp.Surface(0)
	if s.Visibility.Valid {
		t.Error("Surface(0).Visibility 应为缺测")
	}
	if s.BoundaryLayerHeight.Valid {
		t.Error("Surface(0).BoundaryLayerHeight 应为缺测")
	}
	if !s.Temperature2m.Valid {
		t.Error("Surface(0).Temperature2m 不应为缺测")
	}
}

func TestParseResponse_UnknownVariableStaysMissing(t *testing.T) {
	resp := mustParseFixture(t)

	if v := resp.At("soil_temperature_0cm", 0); v.Valid {
		t.Errorf("未请求的变量应返回缺测，实际 %v", v.V)
	}
	if v := resp.At("temperature_2m", 999); v.Valid {
		t.Errorf("越界下标应返回缺测，实际 %v", v.V)
	}
	if v := resp.At("temperature_2m", -1); v.Valid {
		t.Errorf("负下标应返回缺测，实际 %v", v.V)
	}
}

func TestParseResponse_OptionalDropped(t *testing.T) {
	vars := BuildHourlyVars(false)
	resp, err := ParseResponse(loadFixture(t), vars)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	for _, name := range SurfaceOptional {
		if _, ok := resp.Series[name]; ok {
			t.Errorf("未请求的可选变量 %s 不应出现在 Series 里", name)
		}
		if resp.At(name, 0).Valid {
			t.Errorf("%s 应返回缺测", name)
		}
	}
	if !resp.At("temperature_2m", 0).Valid {
		t.Error("必需变量 temperature_2m 不应受影响")
	}
	if got, want := resp.HourlyVars, vars; len(got) != len(want) {
		t.Errorf("HourlyVars 长度 = %d，期望 %d", len(got), len(want))
	}
}

func TestParseResponse_SurfaceAndLevelNotConfused(t *testing.T) {
	const utcOffset int32 = 28800

	localMidnight := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).Unix()

	data := buildStream(t, midnightEpoch(localMidnight, utcOffset), 3600, utcOffset,
		"Asia/Shanghai", []fbVar{
			{variable: openmeteo.Variablerelative_humidity, altitude: 2,
				values: []float32{11, 12}},
			{variable: openmeteo.Variablerelative_humidity, plevel: 850,
				values: []float32{81, 82}},
			{variable: openmeteo.Variablerelative_humidity, plevel: 700,
				values: []float32{71, 72}},
			{variable: openmeteo.Variablecloud_cover, plevel: 850,
				values: []float32{51, 52}},
			{variable: openmeteo.Variablecloud_cover_low,
				values: []float32{5, 6}},
		})

	resp, err := ParseResponse(data, BuildHourlyVars(true))
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}

	cases := []struct {
		name string
		want float64
	}{
		{"relative_humidity_2m", 11},
		{"relative_humidity_850hPa", 81},
		{"relative_humidity_700hPa", 71},
		{"cloud_cover_850hPa", 51},
		{"cloud_cover_low", 5},
	}
	for _, c := range cases {
		got := resp.At(c.name, 0)
		if !got.Valid || got.V != c.want {
			t.Errorf("%s[0] = %v（valid=%v），期望 %v", c.name, got.V, got.Valid, c.want)
		}
	}

	if v := resp.At("relative_humidity_1000hPa", 0); v.Valid {
		t.Errorf("relative_humidity_1000hPa 应缺测，实际 %v", v.V)
	}
}

func TestParseResponse_TimezoneAssertion(t *testing.T) {
	const utcOffset int32 = 28800
	localMidnight := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).Unix()

	tests := []struct {
		name       string
		startLocal int64
		wantErr    bool
	}{
		{"当地 00:00 正常通过", localMidnight, false},
		{"当地 01:00 必须报错", localMidnight + 3600, true},
		{"当地 08:00（时区未生效的典型症状）必须报错", localMidnight + 8*3600, true},
		{"当地 23:00 必须报错", localMidnight - 3600, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := buildStream(t, midnightEpoch(tc.startLocal, utcOffset), 3600, utcOffset,
				"Asia/Shanghai", []fbVar{
					{variable: openmeteo.Variabletemperature, altitude: 2,
						values: []float32{20, 21}},
				})
			resp, err := ParseResponse(data, BuildHourlyVars(true))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际解析成功（首时次 %s）",
						resp.Times[0].Format(TimeLayout))
				}
				if !strings.Contains(err.Error(), "时区校正异常") {
					t.Errorf("错误信息应指明时区问题，实际：%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("期望解析成功，实际报错：%v", err)
			}
			if got := resp.Times[0].Format(TimeLayout); got != "2026-08-12T00:00" {
				t.Errorf("Times[0] = %s", got)
			}
		})
	}
}

func TestParseResponse_MalformedInput(t *testing.T) {
	real := loadFixture(t)

	overLong := make([]byte, 64)
	copy(overLong, real[:64])
	binary.LittleEndian.PutUint32(overLong[:4], 1<<20)

	garbage := make([]byte, 4+64)
	binary.LittleEndian.PutUint32(garbage[:4], 64)
	for i := 4; i < len(garbage); i++ {
		garbage[i] = 0xFF
	}

	truncated := append([]byte(nil), real[:len(real)/2]...)

	halfPatched := append([]byte(nil), real[:len(real)/2]...)
	binary.LittleEndian.PutUint32(halfPatched[:4], uint32(len(halfPatched)-4))

	tooShort := make([]byte, 4+8)
	binary.LittleEndian.PutUint32(tooShort[:4], 8)

	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"空字节", []byte{}},
		{"不足 4 字节的长度前缀", []byte{0x01, 0x02}},
		{"长度前缀为 0", []byte{0, 0, 0, 0}},
		{"长度前缀超出实际长度", overLong},
		{"真实报文被截断", truncated},
		{"消息体过短", tooShort},
		{"前缀合法但消息体是垃圾", garbage},
		{"消息体全 0", prefixed(make([]byte, 64))},
		{"截断后前缀被修正（内部偏移越界）", halfPatched},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := ParseResponse(tc.data, BuildHourlyVars(true))
			if err == nil {
				t.Fatalf("期望报错，实际解析成功：%+v", resp)
			}
			if resp != nil {
				t.Errorf("出错时应返回 nil Response，实际 %+v", resp)
			}
		})
	}

	panicCases := []struct {
		name string
		data []byte
	}{
		{"垃圾消息体", garbage},
		{"截断后前缀被修正", halfPatched},
	}
	for _, tc := range panicCases {
		_, err := ParseResponse(tc.data, BuildHourlyVars(true))
		if err == nil || !strings.Contains(err.Error(), "报文畸形") {
			t.Errorf("%s：期望 panic 被收敛成含「报文畸形」的 error，实际：%v", tc.name, err)
		}
	}
}

func TestParseResponse_JSONErrorBody(t *testing.T) {
	body := []byte(`{"reason":"Data corrupted at path ''. Cannot initialize ... from invalid String value bogus_var_xyz.","error":true}`)
	_, err := ParseResponse(body, BuildHourlyVars(true))
	if err == nil {
		t.Fatal("期望报错")
	}
	if !strings.Contains(err.Error(), "bogus_var_xyz") {
		t.Errorf("错误信息应带上服务端给的原因，实际：%v", err)
	}
}

func TestParseResponse_StructuralErrors(t *testing.T) {
	const utcOffset int32 = 28800
	localMidnight := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).Unix()
	start := midnightEpoch(localMidnight, utcOffset)

	tests := []struct {
		name    string
		data    []byte
		wantSub string
	}{
		{
			name:    "缺少 hourly 数据块",
			data:    buildStreamWithoutHourly(t),
			wantSub: "hourly",
		},
		{
			name: "interval 为 0",
			data: buildStream(t, start, 0, utcOffset, "Asia/Shanghai", []fbVar{
				{variable: openmeteo.Variabletemperature, altitude: 2, values: []float32{20}},
			}),
			wantSub: "interval",
		},
		{
			name: "只返回了本工具不认识的变量",
			data: buildStream(t, start, 3600, utcOffset, "Asia/Shanghai", []fbVar{
				{variable: openmeteo.Variablesoil_temperature, altitude: 0, values: []float32{20}},
			}),
			wantSub: "没有任何可用的气象变量",
		},
		{
			name: "变量存在但样本数为 0",
			data: buildStream(t, start, 3600, utcOffset, "Asia/Shanghai", []fbVar{
				{variable: openmeteo.Variabletemperature, altitude: 2, values: []float32{}},
			}),
			wantSub: "时间轴为空",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseResponse(tc.data, BuildHourlyVars(true))
			if err == nil {
				t.Fatal("期望报错，实际解析成功")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("错误信息应包含 %q，实际：%v", tc.wantSub, err)
			}
		})
	}
}

func TestParseResponse_MultiMessageStream(t *testing.T) {
	const utcOffset int32 = 28800
	localMidnight := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC).Unix()
	start := midnightEpoch(localMidnight, utcOffset)

	first := buildStream(t, start, 3600, utcOffset, "Asia/Shanghai", []fbVar{
		{variable: openmeteo.Variabletemperature, altitude: 2, values: []float32{1, 2}},
	})
	second := buildStream(t, start, 3600, utcOffset, "Asia/Shanghai", []fbVar{
		{variable: openmeteo.Variabletemperature, altitude: 2, values: []float32{9, 9}},
	})

	resp, err := ParseResponse(append(append([]byte(nil), first...), second...), BuildHourlyVars(true))
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if got := resp.At("temperature_2m", 0); !got.Valid || got.V != 1 {
		t.Errorf("应取第一条消息的数据，实际 %v（valid=%v）", got.V, got.Valid)
	}
}

func TestVarKeyOf(t *testing.T) {
	ok := []struct {
		name string
		want variableKey
	}{
		{"temperature_2m", variableKey{openmeteo.Variabletemperature, 2, 0}},
		{"dew_point_2m", variableKey{openmeteo.Variabledew_point, 2, 0}},
		{"relative_humidity_2m", variableKey{openmeteo.Variablerelative_humidity, 2, 0}},
		{"wind_speed_10m", variableKey{openmeteo.Variablewind_speed, 10, 0}},
		{"cloud_cover_low", variableKey{openmeteo.Variablecloud_cover_low, 0, 0}},
		{"cloud_cover_mid", variableKey{openmeteo.Variablecloud_cover_mid, 0, 0}},
		{"cloud_cover_high", variableKey{openmeteo.Variablecloud_cover_high, 0, 0}},
		{"visibility", variableKey{openmeteo.Variablevisibility, 0, 0}},
		{"boundary_layer_height", variableKey{openmeteo.Variableboundary_layer_height, 0, 0}},
		{"freezing_level_height", variableKey{openmeteo.Variablefreezing_level_height, 0, 0}},
		{"cloud_cover_1000hPa", variableKey{openmeteo.Variablecloud_cover, 0, 1000}},
		{"cloud_cover_700hPa", variableKey{openmeteo.Variablecloud_cover, 0, 700}},
		{"geopotential_height_925hPa", variableKey{openmeteo.Variablegeopotential_height, 0, 925}},
		{"relative_humidity_850hPa", variableKey{openmeteo.Variablerelative_humidity, 0, 850}},
	}
	for _, c := range ok {
		got, found := varKeyOf(c.name)
		if !found {
			t.Errorf("varKeyOf(%q) 未识别", c.name)
			continue
		}
		if got != c.want {
			t.Errorf("varKeyOf(%q) = %+v，期望 %+v", c.name, got, c.want)
		}
	}

	bad := []string{
		"", "temperature", "temperature_2", "soil_temperature_0cm",
		"cloud_cover_abchPa", "cloud_cover_-100hPa", "cloud_cover_99999hPa",
		"unknown_500hPa", "hPa",
	}
	for _, name := range bad {
		if key, found := varKeyOf(name); found {
			t.Errorf("varKeyOf(%q) 不应被识别，却返回了 %+v", name, key)
		}
	}

	for _, name := range BuildHourlyVars(true) {
		if _, found := varKeyOf(name); !found {
			t.Errorf("请求变量 %q 无法映射到 FlatBuffers 三元组", name)
		}
	}

	seen := make(map[variableKey]string)
	for _, name := range BuildHourlyVars(true) {
		key, _ := varKeyOf(name)
		if prev, dup := seen[key]; dup {
			t.Errorf("%q 与 %q 映射到同一个三元组 %+v（会串号）", name, prev, key)
		}
		seen[key] = name
	}
}
