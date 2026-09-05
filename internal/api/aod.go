package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
		Time                 []string           `json:"time"`
		AerosolOpticalDepth  []model.OptFloat   `json:"aerosol_optical_depth"`
	} `json:"hourly"`
}

// jsonErrorBody 对应 Open-Meteo 用 JSON 返回的错误体（即便请求的是正常接口）。
type jsonErrorBody struct {
	Error  bool   `json:"error"`
	Reason string `json:"reason"`
}

// FetchAOD 取站点在 [start,end] 内的逐小时 CAMS 气溶胶光学厚度（AOD）。
//
// 返回以「站点当地墙钟（UTC 承载）」为键的映射，键的口径与 forecast 响应
// Response.Times 完全一致（都是把当地墙钟用 UTC 承载），可直接按下标对齐。
//
// 缺测/单点错误不致命：缺测的时次写成 Invalid 的 OptFloat；整段取数失败才返回 error，
// 调用方据此降级到「AOD 缺失」分支（不静默假装空气通透）。
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
	q.Set("start_date", start.Format("2006-01-02"))
	q.Set("end_date", end.Format("2006-01-02"))
	q.Set("timezone", tz)

	requestURL := endpoint + "?" + q.Encode()
	body, err := c.get(ctx, requestURL)
	if err != nil {
		return nil, fmt.Errorf("[%s] AOD 取数失败：%w", site.Name, err)
	}

	// 服务端偶尔用 JSON 错误体应答，这里先识别，避免被当成正常报文误解析。
	var je jsonErrorBody
	if len(body) > 0 && body[0] == '{' {
		if jerr := json.Unmarshal(body, &je); jerr == nil && je.Error {
			reason := je.Reason
			if reason == "" {
				reason = "（服务端未给出原因）"
			}
			return nil, fmt.Errorf("[%s] AOD 接口返回错误：%s", site.Name, reason)
		}
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
