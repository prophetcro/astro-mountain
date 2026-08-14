package tomorrow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/prophetcro/astro-mountain/internal/model"
)

const ProbeSampleCount = 3

var KnownProbeSite = model.Site{
	Name: "牵牛岗",
	Lat:  30.0260,
	Lon:  119.0070,
	Alt:  1489.9,
}

var probeHeaders = []string{
	"content-type",
	"date",
	"x-ratelimit-limit-day",
	"x-ratelimit-limit-hour",
	"x-ratelimit-remaining-day",
	"x-ratelimit-remaining-hour",
	"x-ratelimit-retry-after",
	"retry-after",
}

type ProbeResult struct {
	HTTPStatus int

	RedactedURL string

	KeySource KeySource

	HourlyCount int

	FirstTime string
	LastTime  string

	RawValues []string

	DetectedUnit Unit
	DetectErr    error
}

type probeResponse struct {
	Timelines struct {
		Hourly []struct {
			Time   string          `json:"time"`
			Values json.RawMessage `json:"values"`
		} `json:"hourly"`
	} `json:"timelines"`
}

func Probe(ctx context.Context, c *Client, site model.Site, w io.Writer) (*ProbeResult, error) {
	if c == nil || !c.HasKey() {
		return nil, ErrNoAPIKey
	}

	requestURL := c.BuildURL(site)
	res := &ProbeResult{
		RedactedURL: RedactURL(requestURL),
		KeySource:   c.keySource,
	}

	fmt.Fprintln(w, "══ Tomorrow.io 单位/基准实测探针 ══")
	fmt.Fprintf(w, "点位     : %s (%.4f, %.4f)  海拔 %.1f m\n",
		site.Name, site.Lat, site.Lon, site.Alt)
	fmt.Fprintf(w, "密钥来源 : %s\n", c.keySource.Describe())
	fmt.Fprintf(w, "请求 URL : %s\n", res.RedactedURL)
	fmt.Fprintln(w, "说明     : 本次**只发 1 次**请求，不走缓存、不重试（配额敏感）。")
	fmt.Fprintln(w)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return res, fmt.Errorf("构造探针请求失败：%w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {

		c.recordQuota(EventFailed, "probe")
		return res, fmt.Errorf("探针请求失败：%s", redactErr(err, c.apiKey))
	}
	defer resp.Body.Close()

	c.recordQuota(quotaKindOf(resp.StatusCode), "probe")

	res.HTTPStatus = resp.StatusCode
	fmt.Fprintf(w, "── HTTP 状态 ──\n%s\n\n", resp.Status)

	fmt.Fprintln(w, "── 响应头（白名单）──")
	printProbeHeaders(w, resp.Header)
	fmt.Fprintln(w)

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return res, fmt.Errorf("读取探针响应体失败：%s", redactErr(err, c.apiKey))
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(w, "── 错误响应体（已脱敏）──")
		fmt.Fprintln(w, snippet(body, c.apiKey))
		return res, fmt.Errorf("探针收到非 200 响应：HTTP %d", resp.StatusCode)
	}

	var pr probeResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return res, fmt.Errorf("解析探针响应失败：%w", err)
	}
	entries := pr.Timelines.Hourly
	res.HourlyCount = len(entries)
	if len(entries) > 0 {
		res.FirstTime = entries[0].Time
		res.LastTime = entries[len(entries)-1].Time
	}

	fmt.Fprintln(w, "── 时间线概览 ──")
	fmt.Fprintf(w, "timelines.hourly 数组长度 : %d\n", res.HourlyCount)
	fmt.Fprintf(w, "首个时次                  : %s\n", orDash(res.FirstTime))
	fmt.Fprintf(w, "末个时次                  : %s\n", orDash(res.LastTime))
	fmt.Fprintln(w, "（首末时次之差即该账号可用的预报时效）")
	fmt.Fprintln(w)

	fmt.Fprintf(w, "── 前 %d 个时次的 values（原样 JSON，未做任何处理）──\n", ProbeSampleCount)
	n := ProbeSampleCount
	if len(entries) < n {
		n = len(entries)
	}
	for i := 0; i < n; i++ {
		raw := string(entries[i].Values)
		res.RawValues = append(res.RawValues, raw)
		fmt.Fprintf(w, "[%d] time=%s\n    %s\n", i, entries[i].Time, raw)
	}
	fmt.Fprintln(w)

	samples, perr := parseForecast(body)
	if perr == nil {
		res.DetectedUnit, res.DetectErr = DetectUnit(rawCloudBaseValues(samples))
	} else {
		res.DetectErr = perr
	}
	fmt.Fprintln(w, "── 量级启发式（仅供参考，不能替代人工确认）──")
	if res.DetectErr != nil {
		fmt.Fprintf(w, "判定失败：%v\n", res.DetectErr)
	} else {
		fmt.Fprintf(w, "猜测单位：%s\n", res.DetectedUnit)
		fmt.Fprintln(w, "注意：本启发式无法区分 m 与 ft（5000ft 与 5000m 都落在同一量级区间）。")
	}
	fmt.Fprintln(w)

	printProbeChecklist(w, site)
	return res, nil
}

func printProbeHeaders(w io.Writer, h http.Header) {
	want := make(map[string]bool, len(probeHeaders))
	for _, k := range probeHeaders {
		want[k] = true
	}
	type kv struct{ k, v string }
	var got []kv
	for k, vs := range h {
		if want[strings.ToLower(k)] {
			got = append(got, kv{k, strings.Join(vs, ", ")})
		}
	}
	if len(got) == 0 {
		fmt.Fprintln(w, "（白名单内的响应头一个都没返回）")
		return
	}
	sort.Slice(got, func(i, j int) bool { return got[i].k < got[j].k })
	for _, e := range got {
		fmt.Fprintf(w, "%-28s: %s\n", e.k, e.v)
	}
}

func printProbeChecklist(w io.Writer, site model.Site) {
	fmt.Fprintln(w, "── 请据此人工判定并回填配置 ──")
	fmt.Fprintln(w, "1) 单位：看上面 cloudBase 的量级")
	fmt.Fprintln(w, "     0.1 ~ 15 之间      → km  （填 tomorrow_cloud_base_unit=\"km\"）")
	fmt.Fprintln(w, "     100 ~ 15000 之间   → m 或 ft，需进一步区分：")
	fmt.Fprintln(w, "       同一时次的 cloudCeiling 与 cloudBase 之差若是整百英尺的倍数，")
	fmt.Fprintln(w, "       多半是 ft；也可对照同时刻 METAR 实况报文的云底高度交叉确认。")
	fmt.Fprintf(w, "2) 基准：本点位海拔 %.1f m。\n", site.Alt)
	fmt.Fprintln(w, "     若 cloudBase 换算成米后普遍小于该海拔，且与地面天气相符 → agl；")
	fmt.Fprintln(w, "     若普遍大于该海拔                                       → msl。")
	fmt.Fprintln(w, "3) 时效：用上面的首末时次之差（小时数）。")
	fmt.Fprintln(w, "4) 配额：看 x-ratelimit-limit-day / -hour 两个响应头。")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "确认后请同步修改这两份文件（build.sh 会校验两者一致）：")
	fmt.Fprintln(w, "  configs/config.json")
	fmt.Fprintln(w, "  internal/config/defaults/config.json")
	fmt.Fprintln(w, "把 api.tomorrow_cloud_base_unit 从 \"auto\" 改成实测值。")
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
