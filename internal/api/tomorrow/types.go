package tomorrow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/model"
)

const EnvAPIKey = "TOMORROW_API_KEY"

const redactedMark = "***"

var secretParams = []string{"apikey", "api_key", "key", "token"}

func isSecretParam(name string) bool {
	lower := strings.ToLower(name)
	for _, s := range secretParams {
		if lower == s {
			return true
		}
	}
	return false
}

type KeySource string

const (
	KeySourceEnv KeySource = "env"

	KeySourceConfig KeySource = "config"

	KeySourceNone KeySource = ""
)

func (s KeySource) Describe() string {
	switch s {
	case KeySourceEnv:
		return "环境变量 " + EnvAPIKey
	case KeySourceConfig:
		return "config.json 的 api.tomorrow_api_key"
	default:
		return "未配置"
	}
}

func ResolveAPIKey(envKey, cfgKey string) (resolved string, source KeySource, ok bool) {
	if v := strings.TrimSpace(envKey); v != "" {
		return v, KeySourceEnv, true
	}
	if v := strings.TrimSpace(cfgKey); v != "" {
		return v, KeySourceConfig, true
	}
	return "", KeySourceNone, false
}

func RedactURL(raw string) string {
	if _, err := url.Parse(raw); err != nil {
		return redactedMark
	}
	out, changed := rewriteQuery(raw, func(name, val string) (string, bool) {
		if isSecretParam(name) {
			return redactedMark, true
		}
		return val, true
	})
	if !changed {
		return raw
	}
	return out
}

func stripAPIKey(raw string) string {
	out, _ := rewriteQuery(raw, func(name, val string) (string, bool) {
		return val, !isSecretParam(name)
	})
	return out
}

func rewriteQuery(raw string, handle func(name, val string) (string, bool)) (string, bool) {
	qStart := strings.IndexByte(raw, '?')
	if qStart < 0 {
		return raw, false
	}
	base, rest := raw[:qStart], raw[qStart+1:]

	fragment := ""
	if f := strings.IndexByte(rest, '#'); f >= 0 {
		rest, fragment = rest[:f], rest[f:]
	}

	pairs := strings.Split(rest, "&")
	kept := make([]string, 0, len(pairs))
	changed := false
	for _, pair := range pairs {
		if pair == "" {
			continue
		}
		name, val := pair, ""
		if eq := strings.IndexByte(pair, '='); eq >= 0 {
			name, val = pair[:eq], pair[eq+1:]
		}
		newVal, keep := handle(name, val)
		if !keep {
			changed = true
			continue
		}
		if newVal != val {
			changed = true
			kept = append(kept, name+"="+newVal)
			continue
		}
		kept = append(kept, pair)
	}
	if !changed {
		return raw, false
	}
	if len(kept) == 0 {
		return base + fragment, true
	}
	return base + "?" + strings.Join(kept, "&") + fragment, true
}

type RoundPlan struct {
	Sites []model.Site

	NeedRequest []bool

	Need int

	CacheHits int

	Decision Decision
}

func (p RoundPlan) Allowed() bool { return p.Decision.Allowed }

func (p RoundPlan) Summary() string {
	return fmt.Sprintf("Tomorrow.io 本轮 %d 个点位：缓存命中 %d，需真实请求 %d；"+
		"配额用量 1 小时 %s、24 小时 %s",
		len(p.Sites), p.CacheHits, p.Need,
		usageFrac(p.Decision.Usage.UsedHour, p.Decision.Usage.Limits.PerHour),
		usageFrac(p.Decision.Usage.UsedDay, p.Decision.Usage.Limits.PerDay))
}

func (c *Client) PlanRound(sites []model.Site) RoundPlan {
	p := RoundPlan{
		Sites:       sites,
		NeedRequest: make([]bool, len(sites)),
	}
	for i, site := range sites {
		if c.Cache.Has(api.KeyOf(c.cacheURL(site))) {
			p.CacheHits++
			continue
		}
		p.NeedRequest[i] = true
		p.Need++
	}
	p.Decision = c.Quota.Budget(p.Need)
	return p
}

type SiteOutcome struct {
	Site model.Site

	Result *SiteResult

	Err error

	DegradeReason string
}

func (o SiteOutcome) OK() bool { return o.Result != nil }

type RoundResult struct {
	Plan RoundPlan

	Outcomes []SiteOutcome

	RoundDegraded bool

	Notice string

	RequestsMade int

	CacheHits int
}

func (r RoundResult) OKCount() int {
	n := 0
	for _, o := range r.Outcomes {
		if o.OK() {
			n++
		}
	}
	return n
}

func (r RoundResult) Complete() bool {
	return len(r.Outcomes) > 0 && r.OKCount() == len(r.Outcomes)
}

func (c *Client) FetchRound(ctx context.Context, sites []model.Site) RoundResult {
	res := RoundResult{Outcomes: make([]SiteOutcome, len(sites))}
	for i, s := range sites {
		res.Outcomes[i].Site = s
	}
	if len(sites) == 0 {
		return res
	}

	if !c.HasKey() {
		return c.degradeAll(res, sites,
			"未配置 Tomorrow.io API key（可设置环境变量 "+EnvAPIKey+"），"+
				"本轮跳过第二轨。Open-Meteo 轨道不受影响。", ErrNoAPIKey)
	}

	plan := c.PlanRound(sites)
	res.Plan = plan
	res.CacheHits = plan.CacheHits
	c.logf("%s", plan.Summary())

	if !plan.Allowed() {
		return c.degradeAll(res, sites, plan.Decision.Message(), plan.Decision.Err)
	}

	for i, site := range sites {
		if ctx.Err() != nil {

			return c.degradeRemaining(res, i,
				"运行被中断，Tomorrow.io 轨道未取完，整轨不予呈现", ctx.Err())
		}
		if plan.NeedRequest[i] {
			res.RequestsMade++
		}
		result, err := c.FetchSite(ctx, site)
		if err == nil {
			res.Outcomes[i].Result = result
			continue
		}
		res.Outcomes[i].Err = err
		res.Outcomes[i].DegradeReason = degradeReasonOf(err)

		if errors.Is(err, ErrRateLimit) {
			return c.degradeRemaining(res, i+1,
				"Tomorrow.io 已限流（HTTP 429），为避免继续消耗配额并产出"+
					"半套双轨数据，本轮剩余点位整体跳过。请等待约 1 小时后重试。",
				err)
		}

		if errors.Is(err, ErrUnauthed) {
			return c.degradeRemaining(res, i+1,
				"Tomorrow.io 拒绝了当前 API key（401/403），本轮剩余点位整体跳过。"+
					"请检查环境变量 "+EnvAPIKey+" 是否为有效密钥。", err)
		}
	}

	if !res.Complete() {
		res.RoundDegraded = true
		res.Notice = fmt.Sprintf(
			"⚠ Tomorrow.io 轨道本轮整体降级：%d 个点位中有 %d 个取数失败。"+
				"半套双轨数据会让人误以为对比是完整的，因此整轨不予呈现。"+
				"Open-Meteo 轨道不受影响，结论照常给出。",
			len(sites), len(sites)-res.OKCount())
		for i := range res.Outcomes {
			if res.Outcomes[i].Result != nil {
				res.Outcomes[i].Result = nil
				res.Outcomes[i].DegradeReason = "同轨其他点位取数失败，整轨降级"
			}
		}
	}
	return res
}

func (c *Client) degradeAll(res RoundResult, sites []model.Site, notice string, err error) RoundResult {
	res.RoundDegraded = true
	res.Notice = notice
	reason := degradeReasonOf(err)
	for i := range res.Outcomes {
		res.Outcomes[i].Result = nil
		res.Outcomes[i].Err = err
		res.Outcomes[i].DegradeReason = reason
	}
	if notice != "" {
		c.logf("%s", notice)
	}
	return res
}

func (c *Client) degradeRemaining(res RoundResult, from int, notice string, err error) RoundResult {
	res.RoundDegraded = true
	res.Notice = notice
	for i := from; i < len(res.Outcomes); i++ {
		if res.Outcomes[i].Err == nil {
			res.Outcomes[i].Err = err
		}
		res.Outcomes[i].Result = nil
		res.Outcomes[i].DegradeReason = "本轮已整体中止"
	}
	for i := 0; i < from; i++ {
		if res.Outcomes[i].Result != nil {
			res.Outcomes[i].Result = nil
			res.Outcomes[i].DegradeReason = "同轨已整体中止，避免半套数据"
		}
	}
	c.logf("%s", notice)
	return res
}

func degradeReasonOf(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNoAPIKey):
		return "未配置 API key"
	case errors.Is(err, ErrUnauthed):
		return "API key 无效或无权限"
	case errors.Is(err, ErrRateLimit):
		return "触发 API 限流"
	case errors.Is(err, ErrQuotaExhausted):
		return "配额不足"
	case isCorrupt(err):
		return "配额台账不可读，保守降级"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "运行被中断"
	default:
		return "取数失败"
	}
}

type Unit string

const (
	UnitAuto Unit = "auto"

	UnitMeter Unit = "m"

	UnitFeet Unit = "ft"

	UnitKilometer Unit = "km"
)

type Datum string

const (
	DatumAGL Datum = "agl"

	DatumMSL Datum = "msl"
)

var (
	ErrUnitUnknown = errors.New("tomorrow: 无法从数值量级判断云底单位")

	ErrUnitUnresolved = errors.New("tomorrow: 单位为 auto，换算前必须先经 DetectUnit 解析")
)

const (
	kmUpperBound = 25.0

	meterUpperBound = 40000.0
)

const feetToMeter = 0.3048

func ParseUnit(s string) (Unit, error) {
	switch Unit(strings.ToLower(strings.TrimSpace(s))) {
	case "", UnitAuto:

		return UnitAuto, nil
	case UnitMeter:
		return UnitMeter, nil
	case UnitFeet:
		return UnitFeet, nil
	case UnitKilometer:
		return UnitKilometer, nil
	default:
		return UnitAuto, fmt.Errorf(
			"tomorrow: 无法识别的云底单位 %q（可选 auto / m / ft / km）", s)
	}
}

func ParseDatum(s string) (Datum, error) {
	switch Datum(strings.ToLower(strings.TrimSpace(s))) {
	case "", DatumAGL:
		return DatumAGL, nil
	case DatumMSL:
		return DatumMSL, nil
	default:
		return DatumAGL, fmt.Errorf(
			"tomorrow: 无法识别的云底高度基准 %q（可选 agl / msl）", s)
	}
}

func ToMeters(v float64, u Unit) (float64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("tomorrow: 云底数值不是有限数（%v）", v)
	}
	switch u {
	case UnitMeter:
		return v, nil
	case UnitKilometer:
		return v * 1000.0, nil
	case UnitFeet:
		return v * feetToMeter, nil
	case UnitAuto:
		return 0, ErrUnitUnresolved
	default:
		return 0, fmt.Errorf("tomorrow: 无法识别的云底单位 %q", string(u))
	}
}

func DetectUnit(samples []float64) (Unit, error) {
	maxAbs := 0.0
	seen := false
	for _, v := range samples {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		seen = true
		if a := math.Abs(v); a > maxAbs {
			maxAbs = a
		}
	}
	if !seen {
		return UnitAuto, fmt.Errorf("%w：没有任何有效样本", ErrUnitUnknown)
	}
	switch {
	case maxAbs <= kmUpperBound:
		return UnitKilometer, nil
	case maxAbs <= meterUpperBound:

		fmt.Fprintln(os.Stderr,
			"[tomorrow] 单位按量级自动推断为 m（auto 模式）：无法区分 m 与 ft，"+
				"若数据源实际为英尺，云底高度将偏小约 3.28 倍。"+
				"建议用 --tomorrow-probe 实测后显式配置 api.tomorrow_cloud_base_unit。")
		return UnitMeter, nil
	default:
		return UnitAuto, fmt.Errorf("%w：最大样本值 %g 超出米制合理上限 %g",
			ErrUnitUnknown, maxAbs, meterUpperBound)
	}
}

var ErrUnitMismatch = errors.New("tomorrow: 返回值量级与配置的云底单位不符")

func CheckUnitSanity(configured Unit, samples []float64) error {
	if configured == UnitAuto {
		return nil
	}
	detected, err := DetectUnit(samples)
	if err != nil {

		return nil
	}

	configuredIsKm := configured == UnitKilometer
	detectedIsKm := detected == UnitKilometer
	if configuredIsKm == detectedIsKm {
		return nil
	}
	return fmt.Errorf("%w：配置为 %q，但样本量级看起来是 %q"+
		"（最大值 %g）。若上游确实改了单位，请同步修改两份 config.json 的 "+
		"api.tomorrow_cloud_base_unit；在此之前本轮换算结果不可信",
		ErrUnitMismatch, string(configured), string(detected), maxAbsOf(samples))
}

func maxAbsOf(samples []float64) float64 {
	maxAbs := 0.0
	for _, v := range samples {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if a := math.Abs(v); a > maxAbs {
			maxAbs = a
		}
	}
	return maxAbs
}

func ToMSL(heightM, siteAltM float64, d Datum) float64 {
	if d == DatumMSL {
		return heightM
	}
	return heightM + siteAltM
}

func ToAGL(mslM, siteAltM float64) float64 {
	return mslM - siteAltM
}
