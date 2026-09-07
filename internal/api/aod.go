package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/prophetcro/astro-mountain/internal/model"
)

// AirQualityEndpoint 是 Open-Meteo 空气质量 API（CAMS 气溶胶光学厚度数据源）。
// 该端点免费、无需密钥；与预报端点分离，便于测试时单独打桩。
const AirQualityEndpoint = "https://air-quality-api.open-meteo.com/v1/air-quality"

// aodResponse 是空气质量 API 的精简解析结构，只取朝霞判定需要的逐小时 AOD。
type aodResponse struct {
	Hourly struct {
		Time                []string         `json:"time"`
		AerosolOpticalDepth []model.OptFloat `json:"aerosol_optical_depth"`
	} `json:"hourly"`
}

// jsonErrorBody 对应 Open-Meteo 用 JSON 返回的错误体（即便请求的是正常接口）。
type jsonErrorBody struct {
	Error  bool   `json:"error"`
	Reason string `json:"reason"`
}

// aodRangeRe 匹配 Open-Meteo 错误体里的「允许日期范围」：
// "Parameter 'end_date' is out of allowed range from 2013-01-01 to 2026-09-13"
// （有时写成 "rangefrom"，故 to 两侧允许空白）。
var aodRangeRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})\s+to\s+(\d{4}-\d{2}-\d{2})`)

// aodRangeClamp 从 AOD 接口的错误 reason 里解析 CAMS 允许的最大日期，
// 把请求窗口 [start,end] 夹紧到 [start, min(end, maxDate)]。
// 返回夹紧后的窗口与 ok：
//   - 正则未匹配 → ok=false（非时效类错误，不重试）；
//   - 请求起点已在时效之外（start > maxDate，整体超出）→ ok=false（重试无意义）；
//   - 夹紧后窗口与入参完全相同（end 本就在时效内）→ ok=false（重试无意义）；
//   其余（至少 start..min(end,maxDate) 落在时效内）→ ok=true（应重试一次）。
func aodRangeClamp(reason string, start, end time.Time) (time.Time, time.Time, bool) {
	m := aodRangeRe.FindStringSubmatch(reason)
	if m == nil {
		return start, end, false
	}
	maxDate, err := time.ParseInLocation("2006-01-02", m[2], time.UTC)
	if err != nil {
		return start, end, false
	}
	maxDate = time.Date(maxDate.Year(), maxDate.Month(), maxDate.Day(), 0, 0, 0, 0, time.UTC)
	// 请求起点已在时效之外（整体超出），重试拿到的也是无关日期 → 不重试。
	if start.After(maxDate) {
		return start, end, false
	}
	cs := start
	ce := end
	if end.After(maxDate) {
		ce = maxDate
	}
	// 夹紧后窗口与入参完全相同（end 本就在时效内，重试无意义）。
	if cs.Equal(start) && ce.Equal(end) {
		return start, end, false
	}
	return cs, ce, true
}

// FetchAOD 取站点在 [start,end] 内的逐小时 CAMS 气溶胶光学厚度（AOD）。
//
// 返回以「站点当地墙钟（UTC 承载）」为键的映射，键的口径与 forecast 响应
// Response.Times 完全一致（都是把当地墙钟用 UTC 承载），可直接按下标对齐。
//
// 缺测/单点错误不致命：缺测的时次写成 Invalid 的 OptFloat；整段取数失败才返回 error，
// 调用方据此降级到「AOD 缺失」分支（不静默假装空气通透）。
//
// CAMS 空气质量 API 的滚动预报时效常比天气 API 短（例如最远仅到 2026-09-13），
// 而日出报告为覆盖最晚日出当天会把 end 多算一天 → end_date 超窗返回 HTTP 400。
// 错误体已把允许的最大日期写在 reason 里，本函数据此把窗口夹紧后重试一次，
// 争取拿到时效内的那部分 AOD（日出当天仍在窗内），而不是整站静默丢弃。
// 非时效类错误不重试；完全超出时效（夹紧后无可用窗口）则如实返回错误。
func (c *Client) FetchAOD(ctx context.Context, site model.Site, start, end time.Time) (map[time.Time]model.OptFloat, error) {
	tz := c.Timezone
	if site.Timezone != "" {
		tz = site.Timezone
	}
	endpoint := c.AirQualityEndpoint
	if endpoint == "" {
		endpoint = AirQualityEndpoint
	}

	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(site.Lat, 'f', -1, 64))
	q.Set("longitude", strconv.FormatFloat(site.Lon, 'f', -1, 64))
	q.Set("hourly", "aerosol_optical_depth")
	q.Set("timezone", tz)

	// doFetch 发一次请求并解析「接口错误体」。返回 (body, reason, netErr)：
	//   - netErr != nil：网络/传输层错误；
	//   - reason != ""：服务端用 JSON 错误体应答（error:true），reason 即其原因；
	//   - 二者皆空：正常报文，body 可解析。
	doFetch := func(s, e time.Time) ([]byte, string, error) {
		q.Set("start_date", s.Format("2006-01-02"))
		q.Set("end_date", e.Format("2006-01-02"))
		requestURL := endpoint + "?" + q.Encode()
		body, err := c.get(ctx, requestURL)
		if err != nil {
			return nil, "", fmt.Errorf("[%s] AOD 取数失败：%w", site.Name, err)
		}
		if len(body) > 0 && body[0] == '{' {
			var je jsonErrorBody
			if jerr := json.Unmarshal(body, &je); jerr == nil && je.Error {
				reason := je.Reason
				if reason == "" {
					reason = "（服务端未给出原因）"
				}
				return body, reason, nil
			}
		}
		return body, "", nil
	}

	body, reason, err := doFetch(start, end)
	if err != nil {
		return nil, err
	}
	// 时效超窗 → 按服务端给出的最大日期夹紧窗口重试一次。
	if reason != "" {
		if cs, ce, ok := aodRangeClamp(reason, start, end); ok {
			body, reason, err = doFetch(cs, ce)
			if err != nil {
				return nil, err
			}
		}
	}
	if reason != "" {
		return nil, fmt.Errorf("[%s] AOD 接口返回错误：%s", site.Name, reason)
	}

	var ar aodResponse
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("[%s] 解析 AOD 响应失败：%w", site.Name, err)
	}

	loc, lerr := time.LoadLocation(tz)
	if lerr != nil || loc == nil {
		loc = time.UTC
	}

	out := make(map[time.Time]model.OptFloat, len(ar.Hourly.Time))
	for i, ts := range ar.Hourly.Time {
		t, perr := time.ParseInLocation("2006-01-02T15:04", ts, loc)
		if perr != nil {
			// 个别时次格式异常不影响其余：跳过而非整段失败。
			continue
		}
		key := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
		var v model.OptFloat
		if i < len(ar.Hourly.AerosolOpticalDepth) {
			v = ar.Hourly.AerosolOpticalDepth[i]
		}
		out[key] = v
	}
	return out, nil
}
