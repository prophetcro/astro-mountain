// Package tomorrow 实现 Tomorrow.io 第二轨（B 轨）客户端。
//
// 覆盖：API key 三态解析与 URL 脱敏、单轮多点位取数（全有或全无，避免半套双轨数据）、
// 云底高度的单位与基准换算、跨进程滑动窗口配额台账，以及面向 dualtrack 中立层的适配。
package tomorrow

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

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

const (
	DefaultEndpoint = "https://api.tomorrow.io/v4/weather/forecast"

	maxBodyBytes = 4 << 20

	defaultTimeoutS = 20

	userAgent = "astro-mountain-go/1.0 (cloud-base cross-check)"

	DefaultCacheTTL = 6 * time.Hour
)

var (
	ErrNoAPIKey = errors.New("tomorrow: 未配置 API key，已跳过云底互校")

	ErrDisabled = errors.New("tomorrow: 云底互校已在配置中关闭")

	ErrUnauthed = errors.New("tomorrow: API key 无效或无权限")

	ErrRateLimit = errors.New("tomorrow: 触发 API 限流")
)

type Client struct {
	HTTP     *http.Client
	Endpoint string
	Cache    *api.Cache

	Retries       int
	BackoffFactor float64

	Unit  Unit
	Datum Datum

	Quota *Ledger

	Logf func(format string, args ...any)

	apiKey string

	keySource KeySource

	cfgKey string

	getenv func(string) string

	sleep func(d time.Duration)
}

type Option func(*Client)

func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.HTTP = h }
}

func WithEndpoint(endpoint string) Option {
	return func(c *Client) { c.Endpoint = endpoint }
}

func WithCache(cache *api.Cache) Option {
	return func(c *Client) { c.Cache = cache }
}

func WithLogger(logf func(format string, args ...any)) Option {
	return func(c *Client) { c.Logf = logf }
}

func WithSleep(fn func(d time.Duration)) Option {
	return func(c *Client) { c.sleep = fn }
}

func WithQuotaLedger(l *Ledger) Option {
	return func(c *Client) { c.Quota = l }
}

func WithAPIKey(key string, src KeySource) Option {
	return func(c *Client) {
		c.apiKey = strings.TrimSpace(key)
		c.keySource = src
	}
}

func WithGetenv(getenv func(string) string) Option {
	return func(c *Client) { c.getenv = getenv }
}

func New(cfg config.APIConfig, useCache bool, opts ...Option) *Client {
	timeoutS := cfg.TomorrowTimeoutS
	if timeoutS <= 0 {
		timeoutS = defaultTimeoutS
	}

	cache := api.Disabled()
	if useCache && cfg.CacheEnabled {
		cache = api.NewCache(cfg.CacheDir, tomorrowCacheTTL(cfg))
	}

	c := &Client{
		HTTP:          &http.Client{Timeout: time.Duration(timeoutS) * time.Second},
		Endpoint:      strings.TrimSpace(cfg.TomorrowEndpoint),
		Cache:         cache,
		Retries:       cfg.Retries,
		BackoffFactor: cfg.BackoffFactor,
		sleep:         time.Sleep,
		cfgKey:        cfg.TomorrowAPIKey,

		Quota: NewLedger(cfg.CacheDir, limitsFromConfig(cfg)),
	}
	if c.Endpoint == "" {
		c.Endpoint = DefaultEndpoint
	}

	unit, uerr := ParseUnit(cfg.TomorrowCloudBaseUnit)
	datum, derr := ParseDatum(cfg.TomorrowCloudBaseDatum)
	c.Unit, c.Datum = unit, datum

	for _, opt := range opts {
		opt(c)
	}
	if c.sleep == nil {
		c.sleep = time.Sleep
	}

	if c.apiKey == "" {
		getenv := c.getenv
		if getenv == nil {
			getenv = os.Getenv
		}
		c.apiKey, c.keySource, _ = ResolveAPIKey(getenv(EnvAPIKey), c.cfgKey)
	}
	if uerr != nil {
		c.logf("%v，已退回 auto", uerr)
	}
	if derr != nil {
		c.logf("%v，已退回 agl", derr)
	}
	return c
}

func tomorrowCacheTTL(cfg config.APIConfig) time.Duration {
	if cfg.TomorrowCacheExpireS < 0 {
		return 0
	}
	if cfg.TomorrowCacheExpireS == 0 {
		return DefaultCacheTTL
	}
	return time.Duration(cfg.TomorrowCacheExpireS) * time.Second
}

func limitsFromConfig(cfg config.APIConfig) Limits {
	l := DefaultLimits()
	if cfg.TomorrowQuotaPerHour != 0 {
		l.PerHour = cfg.TomorrowQuotaPerHour
	}
	if cfg.TomorrowQuotaPerDay != 0 {
		l.PerDay = cfg.TomorrowQuotaPerDay
	}
	if cfg.TomorrowMinIntervalMS != 0 {
		l.MinInterval = time.Duration(cfg.TomorrowMinIntervalMS) * time.Millisecond
	}
	return l.normalized()
}

func (c *Client) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

func (c *Client) HasKey() bool { return c != nil && c.apiKey != "" }

func (c *Client) KeySource() KeySource {
	if c == nil {
		return KeySourceNone
	}
	return c.keySource
}

func (c *Client) BuildURL(site model.Site) string {
	q := url.Values{}
	q.Set("location", strconv.FormatFloat(site.Lat, 'f', -1, 64)+","+
		strconv.FormatFloat(site.Lon, 'f', -1, 64))
	q.Set("timesteps", "1h")

	q.Set("units", "metric")
	q.Set("apikey", c.apiKey)
	return c.Endpoint + "?" + q.Encode()
}

func (c *Client) cacheURL(site model.Site) string {
	return stripAPIKey(c.BuildURL(site))
}

type SiteResult struct {
	Site model.Site

	Samples []Sample

	ResolvedUnit Unit

	UnitGuessed bool

	UnitWarning error

	CloudBaseAGL    []model.OptFloat
	CloudCeilingAGL []model.OptFloat

	CloudBaseMSL    []model.OptFloat
	CloudCeilingMSL []model.OptFloat

	MissingRate float64

	FromCache bool
}

func (c *Client) FetchSite(ctx context.Context, site model.Site) (*SiteResult, error) {
	if c == nil {
		return nil, ErrNoAPIKey
	}
	if !c.HasKey() {
		return nil, ErrNoAPIKey
	}

	body, fromCache, err := c.get(ctx, c.BuildURL(site), c.cacheURL(site), site.Name)
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", site.Name, err)
	}
	res, err := c.buildResult(site, body)
	if err != nil {
		return nil, err
	}
	res.FromCache = fromCache
	return res, nil
}

func (c *Client) buildResult(site model.Site, body []byte) (*SiteResult, error) {
	samples, err := parseForecast(body)
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", site.Name, err)
	}

	unit, guessed, err := c.resolveUnit(samples)
	if err != nil {
		return nil, fmt.Errorf("[%s] %w", site.Name, err)
	}

	res := &SiteResult{
		Site:            site,
		Samples:         samples,
		ResolvedUnit:    unit,
		UnitGuessed:     guessed,
		MissingRate:     MissingRate(samples),
		CloudBaseAGL:    make([]model.OptFloat, len(samples)),
		CloudCeilingAGL: make([]model.OptFloat, len(samples)),
		CloudBaseMSL:    make([]model.OptFloat, len(samples)),
		CloudCeilingMSL: make([]model.OptFloat, len(samples)),
	}

	if !guessed {
		if warn := CheckUnitSanity(unit, rawCloudBaseValues(samples)); warn != nil {
			res.UnitWarning = warn
			c.logf("[%s] %v", site.Name, warn)
		}
	}

	for i, s := range samples {
		res.CloudBaseAGL[i], res.CloudBaseMSL[i] = convertHeight(s.CloudBaseRaw, unit, site.Alt, c.Datum)
		res.CloudCeilingAGL[i], res.CloudCeilingMSL[i] = convertHeight(s.CloudCeilingRaw, unit, site.Alt, c.Datum)
	}
	return res, nil
}

func convertHeight(raw model.OptFloat, unit Unit, siteAlt float64, datum Datum) (agl, msl model.OptFloat) {
	if !raw.Valid {
		return model.Missing(), model.Missing()
	}
	meters, err := ToMeters(raw.V, unit)
	if err != nil {
		return model.Missing(), model.Missing()
	}

	if datum == DatumMSL {
		return model.Num(ToAGL(meters, siteAlt)), model.Num(meters)
	}
	return model.Num(meters), model.Num(ToMSL(meters, siteAlt, datum))
}

func (c *Client) resolveUnit(samples []Sample) (Unit, bool, error) {
	if c.Unit != UnitAuto {
		return c.Unit, false, nil
	}
	unit, err := DetectUnit(rawCloudBaseValues(samples))
	if err != nil {
		return UnitAuto, true, err
	}
	c.logf("云底单位未在配置中指定，按量级启发式判定为 %q（报告将标注 WARN）；"+
		"建议用 --tomorrow-probe 实测后钉死 api.tomorrow_cloud_base_unit", string(unit))
	return unit, true, nil
}

func (c *Client) get(ctx context.Context, requestURL, cacheKeyURL, note string) ([]byte, bool, error) {
	key := api.KeyOf(cacheKeyURL)
	if data, ok := c.Cache.Get(key); ok {
		return data, true, nil
	}

	attempts := c.Retries
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			wait := time.Duration(c.BackoffFactor*float64(int(1)<<(attempt-1))*1000) * time.Millisecond
			if wait > 0 {
				c.sleep(wait)
			}
		}
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}

		c.Quota.Throttle()

		body, retryable, err := c.doOnce(ctx, requestURL, note)
		if err == nil {
			if putErr := c.Cache.Put(key, body); putErr != nil {
				c.logf("Tomorrow.io 缓存写入失败（不影响本次结果）：%v", putErr)
			}
			return body, false, nil
		}
		lastErr = err
		if !retryable {
			return nil, false, err
		}
	}
	return nil, false, fmt.Errorf("重试 %d 次后仍失败：%w", attempts, lastErr)
}

func (c *Client) doOnce(ctx context.Context, requestURL, note string) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {

		return nil, false, fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {

		c.recordQuota(EventFailed, note)

		return nil, true, fmt.Errorf("请求 Tomorrow.io 失败（%s）：%v",
			RedactURL(requestURL), redactErr(err, c.apiKey))
	}
	defer resp.Body.Close()

	c.recordQuota(quotaKindOf(resp.StatusCode), note)

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, true, fmt.Errorf("读取 Tomorrow.io 响应体失败：%v", redactErr(err, c.apiKey))
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return body, false, nil
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden:
		return nil, false, fmt.Errorf("%w（HTTP %d）", ErrUnauthed, resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, false, fmt.Errorf("%w（HTTP 429）", ErrRateLimit)
	case resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("Tomorrow.io 服务端错误 HTTP %d：%s",
			resp.StatusCode, snippet(body, c.apiKey))
	default:
		return nil, false, fmt.Errorf("Tomorrow.io 返回 HTTP %d：%s",
			resp.StatusCode, snippet(body, c.apiKey))
	}
}

func quotaKindOf(status int) EventKind {
	switch {
	case status == http.StatusOK:
		return EventOK
	case status == http.StatusTooManyRequests:
		return EventRateLimited
	default:
		return EventFailed
	}
}

func (c *Client) recordQuota(kind EventKind, note string) {
	if err := c.Quota.Record(kind, note); err != nil {
		c.logf("Tomorrow.io 配额台账写入失败，后续请求将保守降级：%v", err)
	}
}

func snippet(body []byte, key string) string {
	const limit = 200
	s := strings.TrimSpace(string(body))
	if runes := []rune(s); len(runes) > limit {
		s = string(runes[:limit]) + "…"
	}
	return scrub(s, key)
}

func redactErr(err error, key string) string {
	if err == nil {
		return ""
	}
	return scrub(err.Error(), key)
}

func scrub(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, redactedMark)
}
