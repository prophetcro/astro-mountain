package tomorrow

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

func testSites(n int) []model.Site {
	out := make([]model.Site, n)
	for i := range out {
		out[i] = model.Site{
			Name: fmt.Sprintf("点位%02d", i+1),
			Lat:  38.0 + float64(i)*0.1,
			Lon:  93.0 + float64(i)*0.1,
			Alt:  2790,
		}
	}
	return out
}

func roundServer(t *testing.T, handler func(n int32, w http.ResponseWriter)) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		handler(n, w)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func writeOK(w http.ResponseWriter) {
	_, _ = io.WriteString(w, liveBody)
}

func fastLedger(dir string, limits Limits) *Ledger {
	return NewLedger(dir, limits, WithLedgerSleep(func(time.Duration) {}))
}

func newRoundClient(t *testing.T, srv *httptest.Server, dir string, limits Limits) *Client {
	t.Helper()
	cfg := config.APIConfig{
		TomorrowEndpoint:      srv.URL,
		CacheDir:              dir,
		TomorrowQuotaPerHour:  limits.PerHour,
		TomorrowQuotaPerDay:   limits.PerDay,
		TomorrowCloudBaseUnit: string(UnitKilometer),
		Retries:               1,
	}
	return New(cfg, false,
		WithAPIKey("k", KeySourceEnv),
		WithHTTPClient(srv.Client()),
		WithQuotaLedger(fastLedger(dir, limits)),
	)
}

func assertAllOrNothing(t *testing.T, r RoundResult) {
	t.Helper()
	ok := r.OKCount()
	if ok != 0 && ok != len(r.Outcomes) {
		t.Errorf("出现半套数据：%d/%d 个点位有结果，这是被明令禁止的",
			ok, len(r.Outcomes))
	}
	if r.RoundDegraded && ok != 0 {
		t.Errorf("整轮已降级，却仍有 %d 个点位保留了结果", ok)
	}
	if r.RoundDegraded && strings.TrimSpace(r.Notice) == "" {
		t.Error("整轮降级必须给出 Notice，不能默默降级")
	}
}

func TestFetchRoundHappyPathIsComplete(t *testing.T) {
	srv, hits := roundServer(t, func(_ int32, w http.ResponseWriter) { writeOK(w) })
	c := newRoundClient(t, srv, t.TempDir(), DefaultLimits())

	sites := testSites(siteCountPerRound)
	r := c.FetchRound(context.Background(), sites)

	assertAllOrNothing(t, r)
	if !r.Complete() {
		t.Fatalf("全部成功时应 Complete，实际 %d/%d；Notice=%q",
			r.OKCount(), len(sites), r.Notice)
	}
	if r.RoundDegraded {
		t.Error("全部成功不该标记整轮降级")
	}
	if int(*hits) != siteCountPerRound {
		t.Errorf("应恰好发 %d 次请求，实际 %d", siteCountPerRound, *hits)
	}
	if r.RequestsMade != siteCountPerRound {
		t.Errorf("RequestsMade = %d，期望 %d", r.RequestsMade, siteCountPerRound)
	}

	for i, o := range r.Outcomes {
		if !o.OK() {
			t.Fatalf("第 %d 个点位没有结果", i)
		}
		if len(o.Result.CloudBaseMSL) == 0 {
			t.Errorf("第 %d 个点位没有云底 MSL 序列", i)
		}
	}
}

func TestFetchRoundEmptySitesIsNoOp(t *testing.T) {
	srv, hits := roundServer(t, func(_ int32, w http.ResponseWriter) { writeOK(w) })
	c := newRoundClient(t, srv, t.TempDir(), DefaultLimits())

	r := c.FetchRound(context.Background(), nil)
	if r.RoundDegraded || len(r.Outcomes) != 0 {
		t.Errorf("空点位列表应是无操作，实际 %+v", r)
	}
	if *hits != 0 {
		t.Errorf("空点位列表不该发请求，实际 %d 次", *hits)
	}
}

func TestFetchRoundBudgetShortDegradesWholeRoundWithoutAnyRequest(t *testing.T) {
	dir := t.TempDir()
	srv, hits := roundServer(t, func(_ int32, w http.ResponseWriter) { writeOK(w) })

	c := newRoundClient(t, srv, dir, Limits{PerHour: 5, PerDay: 500})

	sites := testSites(siteCountPerRound)
	r := c.FetchRound(context.Background(), sites)

	assertAllOrNothing(t, r)
	if !r.RoundDegraded {
		t.Fatal("额度不足时必须整轮降级")
	}
	if r.OKCount() != 0 {
		t.Errorf("整轮降级后不该有任何点位结果，实际 %d", r.OKCount())
	}
	if *hits != 0 {
		t.Errorf("预检拦下后**一个真实请求都不该发**，实际发了 %d 次", *hits)
	}
	if r.RequestsMade != 0 {
		t.Errorf("RequestsMade 应为 0，实际 %d", r.RequestsMade)
	}

	for _, kw := range []string{"配额", "Tomorrow"} {
		if !strings.Contains(r.Notice, kw) {
			t.Errorf("降级提示缺少关键信息 %q：%s", kw, r.Notice)
		}
	}
}

func TestFetchRoundPartialBudgetStillRefusesWholeRound(t *testing.T) {
	dir := t.TempDir()
	srv, hits := roundServer(t, func(_ int32, w http.ResponseWriter) { writeOK(w) })

	c := newRoundClient(t, srv, dir, DefaultLimits())
	for i := 0; i < 20; i++ {
		if err := c.Quota.Record(EventOK, "预置"); err != nil {
			t.Fatalf("预置用量失败：%v", err)
		}
	}

	r := c.FetchRound(context.Background(), testSites(siteCountPerRound))
	assertAllOrNothing(t, r)
	if !r.RoundDegraded {
		t.Fatal("剩余额度 5 < 需求 13，必须整轮降级")
	}
	if *hits != 0 {
		t.Errorf("不该发任何请求，实际 %d 次", *hits)
	}
}

func TestFetchRoundCorruptLedgerDegradesConservatively(t *testing.T) {
	dir := t.TempDir()
	srv, hits := roundServer(t, func(_ int32, w http.ResponseWriter) { writeOK(w) })
	c := newRoundClient(t, srv, dir, DefaultLimits())

	if err := os.WriteFile(filepath.Join(dir, LedgerFileName), []byte("坏掉的台账"), 0o644); err != nil {
		t.Fatalf("构造损坏台账失败：%v", err)
	}

	r := c.FetchRound(context.Background(), testSites(3))
	assertAllOrNothing(t, r)
	if !r.RoundDegraded {
		t.Fatal("台账读不出来时必须保守降级，宁可不跑也不能把账号打爆")
	}
	if *hits != 0 {
		t.Errorf("保守降级下不该发请求，实际 %d 次", *hits)
	}
}

func TestFetchRoundWithoutKeyDegradesQuietly(t *testing.T) {
	srv, hits := roundServer(t, func(_ int32, w http.ResponseWriter) { writeOK(w) })
	cfg := config.APIConfig{TomorrowEndpoint: srv.URL, CacheDir: t.TempDir()}
	c := New(cfg, false, WithHTTPClient(srv.Client()), WithGetenv(func(string) string { return "" }))

	r := c.FetchRound(context.Background(), testSites(5))
	assertAllOrNothing(t, r)
	if !r.RoundDegraded {
		t.Fatal("无 key 时应整轮降级")
	}
	if *hits != 0 {
		t.Errorf("无 key 不该发任何请求，实际 %d 次", *hits)
	}

	if !strings.Contains(r.Notice, "Open-Meteo") {
		t.Errorf("无 key 提示应说明主轨不受影响：%s", r.Notice)
	}
}

func TestFetchRound429VoidsAlreadyFetchedResults(t *testing.T) {
	srv, hits := roundServer(t, func(n int32, w http.ResponseWriter) {
		if n >= 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"message":"rate limit"}`)
			return
		}
		writeOK(w)
	})
	c := newRoundClient(t, srv, t.TempDir(), DefaultLimits())

	r := c.FetchRound(context.Background(), testSites(6))

	assertAllOrNothing(t, r)
	if !r.RoundDegraded {
		t.Fatal("429 应触发整轮降级")
	}
	if r.OKCount() != 0 {
		t.Errorf("前面已成功的点位必须一并作废，实际仍有 %d 个有结果", r.OKCount())
	}

	if *hits > 3 {
		t.Errorf("429 后应立刻中止，实际共发了 %d 次请求", *hits)
	}
	if !strings.Contains(r.Notice, "429") {
		t.Errorf("降级提示应点明 429：%s", r.Notice)
	}
}

func TestFetchRound429CountsAgainstQuota(t *testing.T) {
	dir := t.TempDir()
	srv, _ := roundServer(t, func(n int32, w http.ResponseWriter) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"message":"rate limit"}`)
	})
	c := newRoundClient(t, srv, dir, DefaultLimits())

	c.FetchRound(context.Background(), testSites(3))

	u, err := c.Quota.Snapshot()
	if err != nil {
		t.Fatalf("读取台账失败：%v", err)
	}
	if u.UsedHour < 1 {
		t.Error("429 必须计入配额，服务端已经扣过费了")
	}
}

func TestFetchRoundUnauthorizedAbortsWholeRound(t *testing.T) {
	srv, hits := roundServer(t, func(_ int32, w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"message":"invalid key"}`)
	})
	c := newRoundClient(t, srv, t.TempDir(), DefaultLimits())

	r := c.FetchRound(context.Background(), testSites(8))
	assertAllOrNothing(t, r)
	if !r.RoundDegraded {
		t.Fatal("401 应触发整轮降级")
	}
	if *hits > 1 {
		t.Errorf("401 后应立刻中止，实际发了 %d 次", *hits)
	}
	if !strings.Contains(r.Notice, EnvAPIKey) {
		t.Errorf("401 提示应告诉用户去检查哪个环境变量：%s", r.Notice)
	}
}

func TestFetchRoundSingleSiteFailureVoidsWholeTrack(t *testing.T) {
	srv, _ := roundServer(t, func(n int32, w http.ResponseWriter) {
		if n == 2 {

			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeOK(w)
	})
	c := newRoundClient(t, srv, t.TempDir(), DefaultLimits())

	r := c.FetchRound(context.Background(), testSites(4))
	assertAllOrNothing(t, r)
	if !r.RoundDegraded {
		t.Fatal("有点位失败时必须整轨降级，不能给半套")
	}
	if r.Complete() {
		t.Error("不该报告 Complete")
	}
	if !strings.Contains(r.Notice, "半套") {
		t.Errorf("降级提示应说明为什么不给半套：%s", r.Notice)
	}
}

func TestFetchRoundCanceledContextDegradesWholeRound(t *testing.T) {
	srv, _ := roundServer(t, func(_ int32, w http.ResponseWriter) { writeOK(w) })
	c := newRoundClient(t, srv, t.TempDir(), DefaultLimits())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := c.FetchRound(ctx, testSites(5))
	assertAllOrNothing(t, r)
	if !r.RoundDegraded {
		t.Fatal("上下文取消应整轮降级")
	}
}

func TestFetchRoundCacheHitsDoNotConsumeBudget(t *testing.T) {
	dir := t.TempDir()
	srv, hits := roundServer(t, func(_ int32, w http.ResponseWriter) { writeOK(w) })

	cfg := config.APIConfig{
		TomorrowEndpoint:      srv.URL,
		CacheDir:              dir,
		CacheEnabled:          true,
		TomorrowQuotaPerHour:  DefaultPerHour,
		TomorrowQuotaPerDay:   DefaultPerDay,
		TomorrowMinIntervalMS: 0,
		TomorrowCloudBaseUnit: string(UnitKilometer),
		Retries:               1,
	}
	c := New(cfg, true,
		WithAPIKey("k", KeySourceEnv),
		WithHTTPClient(srv.Client()),
		WithQuotaLedger(fastLedger(dir, DefaultLimits())))

	sites := testSites(3)

	first := c.FetchRound(context.Background(), sites)
	if !first.Complete() {
		t.Fatalf("第一轮应完整，Notice=%q", first.Notice)
	}
	firstHits := *hits
	if firstHits != 3 {
		t.Fatalf("第一轮应发 3 次请求，实际 %d", firstHits)
	}

	for c.Quota.Budget(1).Allowed {
		if err := c.Quota.Record(EventOK, "耗额度"); err != nil {
			t.Fatalf("记账失败：%v", err)
		}
	}

	second := c.FetchRound(context.Background(), sites)
	if !second.Complete() {
		t.Fatalf("全缓存命中时即使额度见底也应完整返回，Notice=%q", second.Notice)
	}
	if *hits != firstHits {
		t.Errorf("第二轮不该发任何真实请求，实际新增 %d 次", *hits-firstHits)
	}
	if second.CacheHits != 3 {
		t.Errorf("第二轮应 3 个点位全命中缓存，实际 %d", second.CacheHits)
	}
	if second.RequestsMade != 0 {
		t.Errorf("第二轮 RequestsMade 应为 0，实际 %d", second.RequestsMade)
	}
}

func TestPlanRoundSendsNoRequest(t *testing.T) {
	srv, hits := roundServer(t, func(_ int32, w http.ResponseWriter) { writeOK(w) })
	c := newRoundClient(t, srv, t.TempDir(), DefaultLimits())

	p := c.PlanRound(testSites(siteCountPerRound))
	if *hits != 0 {
		t.Errorf("预检不该发请求，实际 %d 次", *hits)
	}
	if p.Need != siteCountPerRound {
		t.Errorf("Need = %d，期望 %d", p.Need, siteCountPerRound)
	}
	if !p.Allowed() {
		t.Errorf("默认额度下 13 个点位应放行：%s", p.Decision.Reason())
	}
	if !strings.Contains(p.Summary(), "Tomorrow.io") {
		t.Errorf("Summary 应可读：%s", p.Summary())
	}
}
