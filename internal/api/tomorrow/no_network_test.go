package tomorrow

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
)

var (
	externalRequestCount int64
	externalHosts        sync.Map
)

var errQuotaRedLine = fmt.Errorf("配额红线：测试不得访问外部主机")

type quotaGuardTransport struct{ base http.RoundTripper }

func (g quotaGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if isLoopbackURLHost(req.URL.Hostname()) {
		return g.base.RoundTrip(req)
	}
	atomic.AddInt64(&externalRequestCount, 1)
	externalHosts.Store(req.URL.Host, struct{}{})
	return nil, fmt.Errorf("%w：%s", errQuotaRedLine, req.URL.Host)
}

func isLoopbackURLHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func TestMain(m *testing.M) {
	orig := http.DefaultTransport
	http.DefaultTransport = quotaGuardTransport{base: orig}

	code := m.Run()

	http.DefaultTransport = orig

	if n := atomic.LoadInt64(&externalRequestCount); n > 0 {
		var hosts []string
		externalHosts.Range(func(k, _ any) bool {
			hosts = append(hosts, k.(string))
			return true
		})
		sort.Strings(hosts)

		fmt.Fprintf(os.Stderr, `
❌ 配额红线被突破：internal/api/tomorrow 的测试向外部主机发起了 %d 次请求
   目标主机：%v

   Tomorrow.io 免费额度只有 500 次/天，测试套件不得消耗它。
   最常见的原因是新写的用例忘了把端点指向本地 httptest.Server：

       srv := httptest.NewServer(...)
       cfg := config.APIConfig{TomorrowEndpoint: srv.URL}   // ← 这一行不能少

   端点留空时 client.go:185 会回落到 DefaultEndpoint（真实线上地址）。
`, n, hosts)

		if code == 0 {
			code = 1
		}
	}

	os.Exit(code)
}

func TestQuotaGuardItselfWorks(t *testing.T) {
	before := atomic.LoadInt64(&externalRequestCount)

	req, err := http.NewRequest(http.MethodGet, DefaultEndpoint, nil)
	if err != nil {
		t.Fatalf("构造请求失败：%v", err)
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err == nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("守卫没生效：对 api.tomorrow.io 的请求竟然放行了。" +
			"TestMain 里的 http.DefaultTransport 接管失败，整条配额红线形同虚设")
	}

	if got := atomic.LoadInt64(&externalRequestCount) - before; got != 1 {
		t.Errorf("守卫记账 = %d，期望 1", got)
	}

	for _, h := range []string{"127.0.0.1", "localhost", "::1"} {
		if !isLoopbackURLHost(h) {
			t.Errorf("环回主机 %q 被误判为外部，httptest 用例会被误杀", h)
		}
	}
	for _, h := range []string{"api.tomorrow.io", "x.test", "8.8.8.8"} {
		if isLoopbackURLHost(h) {
			t.Errorf("外部主机 %q 被误判为环回，红线会漏放", h)
		}
	}

	atomic.StoreInt64(&externalRequestCount, before)
	externalHosts.Delete(req.URL.Host)
}
