package meteoblue

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// 编译期断言：*Client 必须实现 core.MeteoblueFetcher 中立接口。
// 这样 core 只依赖接口、不 import 本包（vendor 隔离红线）；若签名漂移，编译即报错。
var _ core.MeteoblueFetcher = (*Client)(nil)

// defaultEndpoint 是 Meteoblue Forecast API 的包请求基址（包名以斜杠拼接在后）。
const defaultEndpoint = "https://my.meteoblue.com/packages"

// maxBodyBytes 是单次响应读取上限，防止异常响应把内存吃光。
const maxBodyBytes = 8 << 20

// ErrNoAPIKey 表示未配置 API key，无法取数；调用方据此中止而非回落其它源。
var ErrNoAPIKey = errors.New("meteoblue: 未配置 API key（可设置环境变量 " + EnvAPIKey + " 或在 config.json 填 meteoblue_api_key），本轮跳过")

// Option 用于在 New 之后覆盖客户端的默认装配。
type Option func(*Client)

// WithHTTPClient 替换底层 HTTP 客户端（用于测试打桩）。
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.HTTP = h }
}

// WithLogger 注入进度日志钩子。
func WithLogger(logf func(string, ...any)) Option {
	return func(c *Client) { c.Logf = logf }
}

// WithSleep 替换退避等待实现，供测试免去真实等待。
func WithSleep(fn func(time.Duration)) Option {
	return func(c *Client) { c.sleep = fn }
}

// Client 是 Meteoblue 取数客户端。
// 零值不可用，请用 New 构造；构造后并发只读使用是安全的。
type Client struct {
	HTTP      *http.Client
	Endpoint  string
	APIKey    string
	KeySource string
	Cfg       config.Config
	Logf      func(string, ...any)
	Now       func() time.Time
	sleep     func(time.Duration)
}

// New 按配置构造客户端。apikey 优先取环境变量，其次配置文件；apikey 为空时
// 仍可构造，但 FetchSite 会返回 ErrNoAPIKey，由上层据此中止或降级。
func New(cfg config.Config, opts ...Option) *Client {
	key, src, _ := ResolveAPIKey(os.Getenv(EnvAPIKey), cfg.API.MeteoblueAPIKey)
	endpoint := cfg.API.MeteoblueEndpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	c := &Client{
		HTTP:      http.DefaultClient,
		Endpoint:  endpoint,
		APIKey:    key,
		KeySource: src,
		Cfg:       cfg,
		Now:       time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Name 返回取数器名称，满足 core.MeteoblueFetcher 接口，用于日志与提示。
func (c *Client) Name() string {
	if c.KeySource != "" {
		return "Meteoblue(" + c.KeySource + ")"
	}
	return "Meteoblue"
}

// FetchSite 拉取单个站点的逐小时评估行（Meteoblue 专有评估：分层云量+降水+能见度）。
// start/end 限定时间窗，targetNights 限定观测夜；两者之外的时刻不产生行。
// 未配置 key 时返回 ErrNoAPIKey。
func (c *Client) FetchSite(ctx context.Context, site model.Site,
	start, end time.Time, targetNights map[string]bool) ([]model.HourRow, error) {

	if c.APIKey == "" {
		return nil, ErrNoAPIKey
	}

	days := c.forecastDays(end)
	reqURL := c.buildURL(site, days)
	c.logf("meteoblue: [%s] 请求 %d 天预报（%s）", site.Name, days, c.stripKey(reqURL))

	body, err := c.fetchWithRetry(ctx, reqURL)
	if err != nil {
		return nil, err
	}

	resp, err := ParseResponse(body)
	if err != nil {
		return nil, fmt.Errorf("meteoblue: 解析响应失败：%w", err)
	}

	loc := c.locOf(site)
	rows := EvaluateResponse(site, resp, start, end, targetNights, &c.Cfg, loc)
	c.logf("meteoblue: [%s] 评估 %d 条夜间记录", site.Name, len(rows))
	return rows, nil
}

// forecastDays 计算需要请求的预报天数：从「现在」到 end 的整天数 +1，夹在 [1,7]。
func (c *Client) forecastDays(end time.Time) int {
	now := c.Now
	if now == nil {
		now = time.Now
	}
	days := int(end.Sub(now()).Hours()/24) + 1
	if days < 1 {
		days = 1
	}
	if days > 7 {
		days = 7
	}
	return days
}

// locOf 解析站点时区（默认 Asia/Shanghai），用于把 Meteoblue 返回的本地时间字符串
// 绑定到正确时区，进而推算 UTC 偏移供天文量计算。
func (c *Client) locOf(site model.Site) *time.Location {
	tz := site.Timezone
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc, _ = time.LoadLocation("Asia/Shanghai")
	}
	return loc
}

// buildURL 拼装 Meteoblue 包请求：以逗号组合 basic-3h + clouds-3h（Meteoblue 规定
// 多包用 ',' 分隔，不可误用 '_'）。
//
// 为什么是 3h 而非 1h：免费试用档（Free-trial）仅开放基础包 + clouds-3h/6h/day，
// **不含 clouds-1h**；用 basic-1h,clouds-1h 会直接 HTTP 403。basic-3h+clouds-3h 在
// 免费档与付费档都可用，且两者合并进单个 data_3h 块、各字段按 3h 对齐，解析最干净。
// 付费用户若要 1h 分辨率，把这里换成 basic-1h,clouds-1h 即可（解析层 DataBlockOf
// 已能识别 data_1h，无需改其它代码）。
//
// 其余参数显式锁定单位与时刻格式：
//   windSpeedUnit=m/s  风速用 m/s（这是 Meteoblue 枚举值，不是 "ms"）
//   temperatureUnit=C 温度用摄氏度
//   precipitationUnit=metric 降水用毫米
//   timeformat=iso8601 时刻返回 RFC3339（含时区偏移，如 2026-08-12T22:00:00+08:00）
//   tz=Asia/Shanghai   时刻按本地时区产出，配合 timeformat 让偏移正确
//   forecastDays=N     只取 N 天预报（封顶 7）
func (c *Client) buildURL(site model.Site, days int) string {
	q := url.Values{}
	q.Set("apikey", c.APIKey)
	q.Set("lat", strconv.FormatFloat(site.Lat, 'f', 6, 64))
	q.Set("lon", strconv.FormatFloat(site.Lon, 'f', 6, 64))
	if site.Alt > 0 {
		q.Set("asl", strconv.FormatFloat(site.Alt, 'f', 1, 64))
	}
	q.Set("format", "json")
	q.Set("windSpeedUnit", "m/s")
	q.Set("temperatureUnit", "C")
	q.Set("precipitationUnit", "metric")
	q.Set("timeformat", "iso8601")
	tz := site.Timezone
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	q.Set("tz", tz)
	q.Set("forecastDays", strconv.Itoa(days))
	return c.Endpoint + "/basic-3h,clouds-3h?" + q.Encode()
}

// fetchWithRetry 带退避重试地拉取响应体；4xx 不重试（密钥/参数错误需人工干预）。
func (c *Client) fetchWithRetry(ctx context.Context, reqURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Cfg.API.Retries; attempt++ {
		if attempt > 0 {
			d := time.Duration(float64(attempt) * c.Cfg.API.BackoffFactor * float64(time.Second))
			if c.sleep != nil {
				c.sleep(d)
			} else {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(d):
				}
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		data, rerr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("meteoblue: HTTP %d: %s",
				resp.StatusCode, strings.TrimSpace(string(data)))
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return nil, lastErr
			}
			continue
		}
		return data, nil
	}
	return nil, fmt.Errorf("meteoblue: 重试 %d 次后仍失败：%w", c.Cfg.API.Retries, lastErr)
}

// stripKey 把 URL 中的明文 key 抹掉，供日志使用，避免密钥泄漏。
func (c *Client) stripKey(raw string) string {
	return strings.Replace(raw, "apikey="+c.APIKey, "apikey=***", 1)
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}
