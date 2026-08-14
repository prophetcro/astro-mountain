package api

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/prophetcro/astro-mountain/internal/api/openmeteo"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// TimeLayout 是响应时间轴对外展示用的布局（分钟精度）。
const TimeLayout = "2006-01-02T15:04"

// minFlatBufferMessage 是一条 FlatBuffers 消息的最小合理长度，
// 低于它说明报文被截断或根本不是 FlatBuffers。
const minFlatBufferMessage = 12

// jsonError 对应 Open-Meteo 用 JSON 返回的错误体（即便请求的是 flatbuffers 格式）。
type jsonError struct {
	Error  bool   `json:"error"`
	Reason string `json:"reason"`
}

// Response 是一次取数解析后的结果。
//
// Times 里的时刻是「站点当地墙钟时间」，只是用 UTC 承载：
// 已经把 UTCOffsetSeconds 加进去了，所以直接取 .Hour() 就是当地钟点，
// 不要再做时区转换。
type Response struct {
	Latitude         float64
	Longitude        float64
	Elevation        float64
	UTCOffsetSeconds int
	Timezone         string

	Times []time.Time

	// Series 按 API 变量名索引逐小时序列，长度与 Times 对齐。
	Series map[string][]model.OptFloat

	// HourlyVars 是本次实际请求的变量名，可据此判断可选变量在不在。
	HourlyVars []string
}

// Len 返回时间轴长度。
func (r *Response) Len() int { return len(r.Times) }

// At 取某个变量在第 idx 个时次的值。
// 变量不存在或下标越界都返回缺测，不 panic——响应缺字段是常态。
func (r *Response) At(name string, idx int) model.OptFloat {
	series, ok := r.Series[name]
	if !ok || idx < 0 || idx >= len(series) {
		return model.Missing()
	}
	return series[idx]
}

// Surface 汇总第 idx 个时次的全部地面要素，缺的字段留缺测。
func (r *Response) Surface(idx int) model.Surface {
	return model.Surface{
		Temperature2m:       r.At("temperature_2m", idx),
		DewPoint2m:          r.At("dew_point_2m", idx),
		RelativeHumidity2m:  r.At("relative_humidity_2m", idx),
		CloudCoverLow:       r.At("cloud_cover_low", idx),
		CloudCoverMid:       r.At("cloud_cover_mid", idx),
		CloudCoverHigh:      r.At("cloud_cover_high", idx),
		WindSpeed10m:        r.At("wind_speed_10m", idx),
		Visibility:          r.At("visibility", idx),
		BoundaryLayerHeight: r.At("boundary_layer_height", idx),
		FreezingLevelHeight: r.At("freezing_level_height", idx),
		Precipitation:       r.At("precipitation", idx),

		WeatherCode: r.At("weather_code", idx),
	}
}

// LevelValues 汇总第 idx 个时次各气压层的原始要素，
// 每个 PressureLevels 里的层都会出现在结果中（值可能全缺测）。
func (r *Response) LevelValues(idx int) map[int]model.RawLevel {
	out := make(map[int]model.RawLevel, len(PressureLevels))
	for _, p := range PressureLevels {
		cc, gh, rh := LevelVarNames(p)
		out[p] = model.RawLevel{
			CC: r.At(cc, idx),
			GH: r.At(gh, idx),
			RH: r.At(rh, idx),
		}
	}
	return out
}

// ParseResponse 解析 Open-Meteo 的响应体。
//
// hourlyVars 是本次请求的变量名列表，用来把 FlatBuffers 里无名的序列
// 对回变量名。服务端出错时会改用 JSON 返回，这里一并识别。
//
// FlatBuffers 解码对畸形报文可能 panic，故整体兜住 recover 转成错误：
// 一份坏报文只该让这个站点失败，不该带走整个进程。
func ParseResponse(data []byte, hourlyVars []string) (resp *Response, err error) {

	defer func() {
		if rec := recover(); rec != nil {
			resp = nil
			err = fmt.Errorf("解析 FlatBuffers 响应失败（报文畸形）：%v", rec)
		}
	}()

	if len(data) == 0 {
		return nil, fmt.Errorf("API 响应为空")
	}
	if reason, isErr := decodeJSONError(data); isErr {
		return nil, fmt.Errorf("API 返回错误：%s", reason)
	}

	message, err := firstFlatBufferMessage(data)
	if err != nil {
		return nil, err
	}

	root := openmeteo.GetRootAsWeatherApiResponse(message, flatbuffers.UOffsetT(0))
	hourly := root.Hourly(nil)
	if hourly == nil {
		return nil, fmt.Errorf("API 响应缺少 hourly 数据块")
	}

	interval := int64(hourly.Interval())
	if interval <= 0 {
		return nil, fmt.Errorf("API 响应的 hourly.interval 非法：%d", interval)
	}
	utcOffset := int(root.UtcOffsetSeconds())

	series, count := parseSeries(hourly, hourlyVars)
	if len(series) == 0 {
		return nil, fmt.Errorf("API 响应里没有任何可用的气象变量")
	}
	if count <= 0 {
		return nil, fmt.Errorf("API 返回的时间轴为空")
	}

	// 时间轴：把 UTC 秒加上时区偏移后按 UTC 承载，得到的就是当地墙钟时间。
	start := hourly.Time() + int64(utcOffset)
	times := make([]time.Time, 0, count)
	for i := 0; i < count; i++ {
		times = append(times, time.Unix(start+int64(i)*interval, 0).UTC())
	}
	// 按日取数时首个时次必然是当地 00:00；不是就说明偏移用错了，
	// 与其让后续夜间窗口筛选悄悄错位，不如当场报错。
	if times[0].Hour() != 0 {
		return nil, fmt.Errorf("时区校正异常：首个时次为 %s （期望北京时间 00:00）",
			times[0].Format(TimeLayout))
	}

	return &Response{
		Latitude:         float64(root.Latitude()),
		Longitude:        float64(root.Longitude()),
		Elevation:        float64(root.Elevation()),
		UTCOffsetSeconds: utcOffset,
		Timezone:         string(root.Timezone()),
		Times:            times,
		Series:           series,
		HourlyVars:       hourlyVars,
	}, nil
}

// parseSeries 把 FlatBuffers 里的各条序列还原成「变量名 → 逐小时值」，
// 并返回时间轴长度。
//
// FlatBuffers 的序列不带名字，只能靠变量枚举 + 高度 + 气压层反查；
// 认不出来的序列直接跳过。
func parseSeries(hourly *openmeteo.VariablesWithTime, hourlyVars []string) (map[string][]model.OptFloat, int) {
	wanted := make(map[variableKey]string, len(hourlyVars))
	for _, name := range hourlyVars {
		if key, ok := varKeyOf(name); ok {
			wanted[key] = name
		}
	}

	series := make(map[string][]model.OptFloat, len(hourlyVars))
	for i := 0; i < hourly.VariablesLength(); i++ {
		var vw openmeteo.VariableWithValues
		if !hourly.Variables(&vw, i) {
			continue
		}
		key := variableKey{
			variable:      vw.Variable(),
			altitude:      vw.Altitude(),
			pressureLevel: vw.PressureLevel(),
		}
		name, ok := wanted[key]
		if !ok {
			// 没请求过的变量（或多模式响应里别的模式的量），不收。
			continue
		}
		if _, dup := series[name]; dup {
			// 同名序列只认第一条，避免多模式响应互相覆盖。
			continue
		}
		n := vw.ValuesLength()
		values := make([]model.OptFloat, n)
		for j := 0; j < n; j++ {
			// NaN 在这里转成缺测，下游据此区分「没数」与「值是 0」。
			values[j] = model.NumOrMissing(float64(vw.Values(j)))
		}
		series[name] = values
	}

	count := 0
	// 时间轴长度取第一条拿到的序列长度：所有序列共用同一条时间轴。
	for _, name := range hourlyVars {
		if v, ok := series[name]; ok {
			count = len(v)
			break
		}
	}
	return series, count
}

// firstFlatBufferMessage 剥掉 4 字节小端长度前缀，取出第一条消息。
// 长度声明与实际字节数对不上时报错，防止越界读到脏数据。
func firstFlatBufferMessage(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("API 响应过短（%d 字节），缺少 FlatBuffers 长度前缀", len(data))
	}
	size := binary.LittleEndian.Uint32(data[:4])
	if size == 0 {
		return nil, fmt.Errorf("FlatBuffers 长度前缀为 0，响应不含任何消息")
	}
	if uint64(size) > uint64(len(data)-4) {
		return nil, fmt.Errorf("FlatBuffers 长度前缀声明 %d 字节，实际只有 %d 字节（响应被截断）",
			size, len(data)-4)
	}
	if size < minFlatBufferMessage {
		return nil, fmt.Errorf("FlatBuffers 消息过短（%d 字节），报文畸形", size)
	}
	return data[4 : 4+size], nil
}

// decodeJSONError 识别服务端改用 JSON 返回的错误体，返回原因与是否为错误。
// 不是 JSON 对象、或解析不出 error=true 的，都当作正常二进制响应放行。
func decodeJSONError(data []byte) (string, bool) {

	if len(data) == 0 || data[0] != '{' {
		return "", false
	}
	var je jsonError
	if err := json.Unmarshal(data, &je); err != nil || !je.Error {
		return "", false
	}
	if je.Reason == "" {
		return "（服务端未给出原因）", true
	}
	return je.Reason, true
}
