// Package api 负责向 Open-Meteo 取数：拼请求、带重试地发 HTTP、
// 解析 FlatBuffers 响应，并做本地磁盘缓存。
//
// 对外主入口是 Client.FetchSite：给一个站点和日期区间，返回逐小时的
// 地面要素与气压层廓线序列（Response）。本包只管「拿到数」，
// 不做任何气象判断，评级在 profile / core。
package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

// maxBodyBytes 是单次响应读取上限，防止异常响应把内存吃光。
const maxBodyBytes = 8 << 20

// Client 是 Open-Meteo 取数客户端。
// 零值不可用，请用 New 构造；构造后并发只读使用是安全的。
type Client struct {
	HTTP     *http.Client
	Endpoint string
	Models   string
	Timezone string
	Cache    *Cache

	Retries       int
	BackoffFactor float64

	Logf func(format string, args ...any)

	// sleep 是可替换的等待实现，测试里换成假的即可跳过退避耗时。
	sleep func(d time.Duration)
}

// Option 用于在 New 之后覆盖客户端的默认装配。
type Option func(*Client)

// WithHTTPClient 替换底层 HTTP 客户端（可用于超时定制或测试打桩）。
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.HTTP = h }
}

// WithEndpoint 覆盖 API 端点地址。
func WithEndpoint(endpoint string) Option {
	return func(c *Client) { c.Endpoint = endpoint }
}

// WithCache 替换缓存实现。
func WithCache(cache *Cache) Option {
	return func(c *Client) { c.Cache = cache }
}

// WithLogger 注入进度日志钩子。
func WithLogger(logf func(format string, args ...any)) Option {
	return func(c *Client) { c.Logf = logf }
}

// WithSleep 替换退避等待实现，供测试免去真实等待。
func WithSleep(fn func(d time.Duration)) Option {
	return func(c *Client) { c.sleep = fn }
}

// New 按配置构造客户端。useCache 为 false 或配置关闭缓存时，
// 装配一个永远不命中的空缓存，调用方无需再做 nil 判断。
// 端点、时区、模式为空时各自回落到内置默认值。
func New(cfg config.APIConfig, useCache bool, opts ...Option) *Client {
	cache := Disabled()
	if useCache && cfg.CacheEnabled {
		cache = NewCache(cfg.CacheDir, time.Duration(cfg.CacheExpireS)*time.Second)
	}
	c := &Client{
		HTTP:          &http.Client{Timeout: 60 * time.Second},
		Endpoint:      cfg.Endpoint,
		Models:        cfg.Models,
		Timezone:      cfg.Timezone,
		Cache:         cache,
		Retries:       cfg.Retries,
		BackoffFactor: cfg.BackoffFactor,
		sleep:         time.Sleep,
	}
	if c.Endpoint == "" {
		c.Endpoint = "https://api.open-meteo.com/v1/forecast"
	}
	if c.Timezone == "" {
		c.Timezone = "Asia/Shanghai"
	}
	if c.Models == "" {
		c.Models = "icon_seamless"
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.sleep == nil {
		c.sleep = time.Sleep
	}
	return c
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// BuildURL 拼出一次取数请求的完整 URL。
// models 为空时用客户端默认模式；站点自带时区时优先于客户端时区。
//
// 有三个固定不变的查询参数：elevation 用站点海拔（避免模式按地形网格
// 猜错高度）、wind_speed_unit 固定 m/s、format 固定 flatbuffers。
func (c *Client) BuildURL(site model.Site, start, end time.Time, models string, hourlyVars []string) string {
	if models == "" {
		models = c.Models
	}
	q := url.Values{}
	q.Set("latitude", strconv.FormatFloat(site.Lat, 'f', -1, 64))
	q.Set("longitude", strconv.FormatFloat(site.Lon, 'f', -1, 64))

	q.Set("elevation", strconv.FormatFloat(site.Alt, 'f', -1, 64))
	q.Set("start_date", start.Format("2006-01-02"))
	q.Set("end_date", end.Format("2006-01-02"))
	q.Set("hourly", strings.Join(hourlyVars, ","))
	q.Set("models", models)
	tz := c.Timezone
	if site.Timezone != "" {
		tz = site.Timezone
	}
	q.Set("timezone", tz)

	q.Set("wind_speed_unit", "ms")

	q.Set("format", "flatbuffers")
	return c.Endpoint + "?" + q.Encode()
}

// FetchSite 取回单站点在 [start, end] 内的逐小时数据，
// 同时返回本次真正请求到的变量名列表（调用方据此知道有没有可选变量）。
//
// 采取「先要全、失败再降级」策略：先带上可选变量请求一次，
// 若失败则剔除它们重试——有些模式压根不提供 visibility 之类的量，
// 与其整站取数失败，不如少几个字段。
func (c *Client) FetchSite(ctx context.Context, site model.Site,
	start, end time.Time, models string) (*Response, []string, error) {

	c.logf("站点 %s 使用模型 %s（region=%q）", site.Name, models, site.Region)
	var lastErr error
	for _, includeOptional := range []bool{true, false} {
		hourlyVars := BuildHourlyVars(includeOptional)
		requestURL := c.BuildURL(site, start, end, models, hourlyVars)

		body, err := c.get(ctx, requestURL)
		if err == nil {
			var resp *Response
			resp, err = ParseResponse(body, hourlyVars)
			if err == nil {
				return resp, hourlyVars, nil
			}
		}
		lastErr = err
		// 取消是用户或上层的决定，不该被降级重试掩盖。
		if ctx.Err() != nil {
			return nil, nil, fmt.Errorf("[%s] 请求被取消：%w", site.Name, ctx.Err())
		}
		if includeOptional {
			c.logf("[%s] 含可选变量的请求失败(%v)，剔除 %s 后重试",
				site.Name, err, strings.Join(SurfaceOptional[:], "/"))
			continue
		}
	}
	return nil, nil, fmt.Errorf("[%s] 获取失败：%w", site.Name, lastErr)
}

// get 取一次 URL 的响应体：先查缓存，未命中再带指数退避重试地发请求，
// 成功后写回缓存。
func (c *Client) get(ctx context.Context, requestURL string) ([]byte, error) {
	key := KeyOf(requestURL)
	if data, ok := c.Cache.Get(key); ok {
		return data, nil
	}

	attempts := c.Retries
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			// 指数退避：第 n 次重试前等 BackoffFactor × 2^(n-1) 秒。
			wait := time.Duration(c.BackoffFactor*float64(int(1)<<(attempt-1))*1000) * time.Millisecond
			if wait > 0 {
				c.sleep(wait)
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		body, retryable, err := c.doOnce(ctx, requestURL)
		if err == nil {
			if putErr := c.Cache.Put(key, body); putErr != nil {
				// 缓存只是加速手段，写不进去不影响本次已经拿到的数据。
				c.logf("缓存写入失败（不影响本次结果）：%v", putErr)
			}
			return body, nil
		}
		lastErr = err
		if !retryable {
			return nil, err
		}
	}
	return nil, fmt.Errorf("重试 %d 次后仍失败：%w", attempts, lastErr)
}

// doOnce 发一次请求。第二个返回值表示这个错误是否值得重试：
// 网络抖动、429 与 5xx 可重试，4xx 之类的请求本身有问题则不重试。
func (c *Client) doOnce(ctx context.Context, requestURL string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("构造请求失败：%w", err)
	}

	req.Header.Set("Accept", "application/octet-stream, application/json")
	req.Header.Set("User-Agent", "astro-mountain-go/1.0 (+https://open-meteo.com)")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("请求 Open-Meteo 失败：%w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, true, fmt.Errorf("读取响应体失败：%w", err)
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return body, false, nil
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode >= 500:
		// 限流与服务端故障通常是暂时的，值得等一等再试。
		return nil, true, fmt.Errorf("HTTP %d：%s", resp.StatusCode, snippet(body))
	default:
		// 其余状态码多半是请求本身有问题，重试只会浪费时间。
		return nil, false, fmt.Errorf("HTTP %d：%s", resp.StatusCode, snippet(body))
	}
}

// snippet 把响应体裁成一句可放进错误信息的摘要。
// 文本按字符截断（不会切坏多字节汉字），二进制则只给长度与头部十六进制。
func snippet(body []byte) string {
	const limit = 200
	s := strings.TrimSpace(string(body))
	if isPrintableText(s) {

		if runes := []rune(s); len(runes) > limit {
			return string(runes[:limit]) + "…"
		}
		return s
	}
	const hexLimit = 64
	if len(body) > hexLimit {
		return fmt.Sprintf("<二进制 %d 字节> %x…", len(body), body[:hexLimit])
	}
	return fmt.Sprintf("<二进制 %d 字节> %x", len(body), body)
}

// isPrintableText 判断字符串是否为可直接展示的文本：
// 必须是合法 UTF-8，且除制表/换行/回车外不含控制字符。
func isPrintableText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
