package core

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/prophetcro/astro-mountain/internal/api"
	"github.com/prophetcro/astro-mountain/internal/api/openmeteo"
	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/model"
)

const (
	redlineHours     = 96
	redlineUTCOffset = int32(28800)
	redlineTimezone  = "Asia/Shanghai"
	redlineSentinel  = "0000000000000000000000000000000000000000000000000000000000000000"
)

type redlineFbVar struct {
	variable openmeteo.Variable
	altitude int16
	plevel   int16
	values   []float32
}

func redlineBuildStream(t *testing.T, startEpoch int64, interval int32, utcOffset int32,
	timezone string, vars []redlineFbVar) []byte {
	t.Helper()

	b := flatbuffers.NewBuilder(2048)

	tzOff := b.CreateString(timezone)

	varOffsets := make([]flatbuffers.UOffsetT, 0, len(vars))
	for _, v := range vars {
		openmeteo.VariableWithValuesStartValuesVector(b, len(v.values))
		for i := len(v.values) - 1; i >= 0; i-- {
			b.PrependFloat32(v.values[i])
		}
		valuesOff := b.EndVector(len(v.values))

		openmeteo.VariableWithValuesStart(b)
		openmeteo.VariableWithValuesAddVariable(b, v.variable)
		openmeteo.VariableWithValuesAddAltitude(b, v.altitude)
		openmeteo.VariableWithValuesAddPressureLevel(b, v.plevel)
		openmeteo.VariableWithValuesAddValues(b, valuesOff)
		varOffsets = append(varOffsets, openmeteo.VariableWithValuesEnd(b))
	}

	openmeteo.VariablesWithTimeStartVariablesVector(b, len(varOffsets))
	for i := len(varOffsets) - 1; i >= 0; i-- {
		b.PrependUOffsetT(varOffsets[i])
	}
	varsVec := b.EndVector(len(varOffsets))

	count := 0
	if len(vars) > 0 {
		count = len(vars[0].values)
	}
	openmeteo.VariablesWithTimeStart(b)
	openmeteo.VariablesWithTimeAddTime(b, startEpoch)
	openmeteo.VariablesWithTimeAddTimeEnd(b, startEpoch+int64(count)*int64(interval))
	openmeteo.VariablesWithTimeAddInterval(b, interval)
	openmeteo.VariablesWithTimeAddVariables(b, varsVec)
	hourlyOff := openmeteo.VariablesWithTimeEnd(b)

	openmeteo.WeatherApiResponseStart(b)
	openmeteo.WeatherApiResponseAddLatitude(b, 28.25)
	openmeteo.WeatherApiResponseAddLongitude(b, 119.375)
	openmeteo.WeatherApiResponseAddElevation(b, 1000)
	openmeteo.WeatherApiResponseAddUtcOffsetSeconds(b, utcOffset)
	openmeteo.WeatherApiResponseAddTimezone(b, tzOff)
	openmeteo.WeatherApiResponseAddHourly(b, hourlyOff)
	b.Finish(openmeteo.WeatherApiResponseEnd(b))

	return redlinePrefixed(b.FinishedBytes())
}

func redlinePrefixed(msg []byte) []byte {
	out := make([]byte, 4+len(msg))
	binary.LittleEndian.PutUint32(out[:4], uint32(len(msg)))
	copy(out[4:], msg)
	return out
}

func redlineVarList(cc, rh float32) []redlineFbVar {
	repeat := func(v float32) []float32 {
		s := make([]float32, redlineHours)
		for i := range s {
			s[i] = v
		}
		return s
	}

	gh := map[int]float32{
		1000: 120, 975: 260, 950: 410, 925: 560,
		900: 710, 850: 1200, 800: 1700, 700: 2900,
	}
	vars := []redlineFbVar{
		{openmeteo.Variabletemperature, 2, 0, repeat(20)},
		{openmeteo.Variabledew_point, 2, 0, repeat(12)},
		{openmeteo.Variablerelative_humidity, 2, 0, repeat(rh)},
		{openmeteo.Variablecloud_cover_low, 0, 0, repeat(cc)},
		{openmeteo.Variablecloud_cover_mid, 0, 0, repeat(cc)},
		{openmeteo.Variablecloud_cover_high, 0, 0, repeat(cc)},
		{openmeteo.Variablewind_speed, 10, 0, repeat(3)},
		{openmeteo.Variablevisibility, 0, 0, repeat(12000)},
		{openmeteo.Variableboundary_layer_height, 0, 0, repeat(800)},
		{openmeteo.Variablefreezing_level_height, 0, 0, repeat(3000)},
	}
	for _, p := range []int{1000, 975, 950, 925, 900, 850, 800, 700} {
		vars = append(vars,
			redlineFbVar{openmeteo.Variablecloud_cover, 0, int16(p), repeat(cc)},
			redlineFbVar{openmeteo.Variablegeopotential_height, 0, int16(p), repeat(gh[p])},
			redlineFbVar{openmeteo.Variablerelative_humidity, 0, int16(p), repeat(rh)},
		)
	}
	return vars
}

func redlineStreamFor(t *testing.T, regime string) []byte {
	t.Helper()
	var cc, rh float32 = 2, 45
	if regime == "overcast" {
		cc, rh = 95, 95
	}

	localMidnightUTC := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC).Unix()
	startEpoch := localMidnightUTC - int64(redlineUTCOffset)
	return redlineBuildStream(t, startEpoch, 3600, redlineUTCOffset, redlineTimezone,
		redlineVarList(cc, rh))
}

func redlineOneSite() []model.Site {
	return []model.Site{{Name: "星辰山", Lat: 28.2656, Lon: 119.3788, Alt: 1000.0}}
}

func redlineThreeSites() []model.Site {
	return []model.Site{
		{Name: "星辰山", Lat: 28.2656, Lon: 119.3788, Alt: 1000.0},
		{Name: "牵牛岗", Lat: 30.0260, Lon: 119.0070, Alt: 1489.9},
		{Name: "括苍山", Lat: 28.8101, Lon: 120.9221, Alt: 1382.6},
	}
}

func redlineRunDefault(t *testing.T, sites []model.Site, peak string, days int,
	stream []byte) string {
	t.Helper()

	cfg := config.Default()
	cfg.API.TomorrowEnabled = false
	// 红线测试只校验「默认单模型」的字节一致性，显式关掉双模型对比，
	// 避免第二模型取数改变 CSV/JSON 产物、击穿 parity 基线。
	cfg.API.CrossModel = ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(stream)
	}))
	defer srv.Close()

	client := api.New(cfg.API, false,
		api.WithEndpoint(srv.URL),
		api.WithCache(api.Disabled()),
		api.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
		api.WithSleep(func(time.Duration) {}),
	)

	frozen := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	e := NewEngine(cfg)
	e.Client = client
	e.Now = func() time.Time { return frozen }
	e.Logf = func(string, ...any) {}

	res := e.Run(context.Background(), RunParams{
		Peak:       peak,
		Days:       days,
		Source:     SourceOpenMeteo,
		Sites:      sites,
		NoCache:    true,
		ExportCSV:  true,
		ExportJSON: true,
		Quiet:      true,
		OutDir:     t.TempDir(),
	})
	if res.ExitCode != 0 {
		t.Fatalf("默认轨运行失败（exit=%d）：%v", res.ExitCode, res.Errors)
	}
	if res.CSVPath == "" || res.JSONPath == "" {
		t.Fatalf("默认轨未产出 CSV/JSON 产物：CSVPath=%q JSONPath=%q", res.CSVPath, res.JSONPath)
	}
	return redlineHashFiles(t, res.CSVPath, res.JSONPath)
}

func redlineHashFiles(t *testing.T, paths ...string) string {
	t.Helper()
	h := sha256.New()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("读取产物 %s 失败：%v", p, err)
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

var (
	redlineExternalCount int64
	redlineExternalHosts sync.Map
)

type redlineGuardTransport struct{ base http.RoundTripper }

func (g redlineGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if isLoopbackHost(req.URL.Hostname()) {
		return g.base.RoundTrip(req)
	}
	atomic.AddInt64(&redlineExternalCount, 1)
	redlineExternalHosts.Store(req.URL.Host, struct{}{})
	return nil, fmt.Errorf("红线1守卫：拦截了到 %s 的外部请求（默认轨不应触碰付费 API）", req.URL.Host)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func hostListHasTomorrow(hosts []string) bool {
	for _, h := range hosts {
		if strings.Contains(h, "tomorrow.io") {
			return true
		}
	}
	return false
}

func TestOpenMeteoZeroTomorrowCall(t *testing.T) {
	if testing.Short() {
		t.Skip("红线1 需要构造离线 FlatBuffers 报文，跳过 -short")
	}

	orig := http.DefaultTransport
	http.DefaultTransport = redlineGuardTransport{base: orig}
	defer func() { http.DefaultTransport = orig }()

	atomic.StoreInt64(&redlineExternalCount, 0)
	redlineExternalHosts.Range(func(k, _ any) bool {
		redlineExternalHosts.Delete(k)
		return true
	})

	cfg := config.Default()
	cfg.API.TomorrowEnabled = false

	stream := redlineStreamFor(t, "clear")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(stream)
	}))
	defer srv.Close()

	client := api.New(cfg.API, false,
		api.WithEndpoint(srv.URL),
		api.WithCache(api.Disabled()),
		api.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
		api.WithSleep(func(time.Duration) {}),
	)

	frozen := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	e := NewEngine(cfg)
	e.Client = client
	e.Now = func() time.Time { return frozen }
	e.Logf = func(string, ...any) {}

	res := e.Run(context.Background(), RunParams{
		Peak:    "2026-08-12",
		Days:    0,
		Source:  SourceOpenMeteo,
		Sites:   redlineOneSite(),
		NoCache: true,
		Quiet:   true,
		OutDir:  t.TempDir(),
	})
	if res.ExitCode != 0 {
		t.Fatalf("默认轨运行失败（exit=%d）：%v", res.ExitCode, res.Errors)
	}

	if n := atomic.LoadInt64(&redlineExternalCount); n != 0 {
		var hosts []string
		redlineExternalHosts.Range(func(k, _ any) bool {
			hosts = append(hosts, k.(string))
			return true
		})
		sort.Strings(hosts)

		if hostListHasTomorrow(hosts) {
			t.Fatalf(`红线1突破：默认轨（Open-Meteo）完整运行向外部主机发起了 %d 次出站请求（%v）。
Tomorrow.io 是付费配额 API，默认轨绝不许碰它——这是 D4-6 红线 1。
若这是有意为之（默认轨开始使用 Tomorrow.io），必须先改设计文档并知会团队，
绝不能默默让默认轨烧配额。`, n, hosts)
		}
		t.Fatalf(`红线1突破：默认轨（Open-Meteo）完整运行触达了非预期外部主机（%v）。
默认轨在测试环境下不应联接任何真实外网，Tomorrow.io 只是其中最贵的那个；
其它外部主机同样不该出现。请确认这是有意新增的数据源，还是误接线/误配置。`, hosts)
	}
}

const (
	redlineBaselineClearSingle    = "a495cf6a59c0cc9d5a535e89dc01e1a7e3193b19141dde660ddb89e7584cd9e0"
	redlineBaselineClearMulti     = "d7c3fccc823633ac475ac1342ea688e20cdec54414366b367b1c9fd46ab9352d"
	redlineBaselineOvercastSingle = "5fd5455ff230e06e7a8706eb73835d1dc390a901d035cd60a1c5ed783f7f7190"
	redlineBaselineOvercastMulti  = "9e63117a58fc2454aa1e2779c8faecf479ee6de96ae8de4fd435d1df439c282d"
)

func TestSourceDefaultByteIdentical(t *testing.T) {
	if testing.Short() {
		t.Skip("红线2 需要构造离线 FlatBuffers 报文，跳过 -short")
	}

	type combo struct {
		name     string
		sites    []model.Site
		peak     string
		days     int
		regime   string
		baseline string
	}
	combos := []combo{
		{"单点单夜_通透", redlineOneSite(), "2026-08-12", 0, "clear", redlineBaselineClearSingle},
		{"多点多夜_通透", redlineThreeSites(), "2026-08-13", 2, "clear", redlineBaselineClearMulti},
		{"单点单夜_阴云", redlineOneSite(), "2026-08-12", 0, "overcast", redlineBaselineOvercastSingle},
		{"多点多夜_阴云", redlineThreeSites(), "2026-08-13", 2, "overcast", redlineBaselineOvercastMulti},
	}

	got := make(map[string]string, len(combos))
	for _, c := range combos {
		c := c
		t.Run(c.name, func(t *testing.T) {
			stream := redlineStreamFor(t, c.regime)
			h := redlineRunDefault(t, c.sites, c.peak, c.days, stream)
			got[c.name] = h

			if c.baseline == redlineSentinel {

				t.Logf("基线未冻结，采集到 hash=%s（请填入 redlineBaseline* 常量）", h)
				return
			}
			if h != c.baseline {
				t.Fatalf(`红线2突破（%s）：默认轨输出 sha256 = %s，与冻结基线 %s 不一致。

若这是**有意**的 A 轨变更（新字段/新判据/新文案），请在 PR 说明后更新基线常量；
否则这就是一次回归——默认轨输出变了，parity（432×39）失守。`,
					c.name, h, c.baseline)
			}
		})
	}

	for _, c := range combos {
		for other, otherName := range got {
			if other == got[c.name] && otherName != c.name {
				t.Errorf("红线2灵敏度失效：组合 %q 与 %q 的输出 hash 相同（%s），"+
					"说明默认轨输出没有随输入变化——守卫形同虚设", c.name, otherName, got[c.name])
			}
		}
	}
}

// TestCrossModelCompare 验证双模型交叉对比：ICON 用通透报文、GFS 用阴云报文，
// 配对后 ICON 侧判通透、GFS 侧判不宜，共识应标 icon_only 分歧，且绝不应出现 both_ok。
// 这是对「反向优化」核心诉求（不迷信单模型 ICON）的回归保护。
func TestCrossModelCompare(t *testing.T) {
	if testing.Short() {
		t.Skip("双模型对比需要构造离线 FlatBuffers 报文，跳过 -short")
	}

	cfg := config.Default()
	cfg.API.TomorrowEnabled = false
	cfg.API.CrossModel = "gfs_seamless"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		models := r.URL.Query().Get("models")
		var stream []byte
		if models == "gfs_seamless" {
			stream = redlineStreamFor(t, "overcast")
		} else {
			stream = redlineStreamFor(t, "clear")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(stream)
	}))
	defer srv.Close()

	client := api.New(cfg.API, false,
		api.WithEndpoint(srv.URL),
		api.WithCache(api.Disabled()),
		api.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
		api.WithSleep(func(time.Duration) {}),
	)

	frozen := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	e := NewEngine(cfg)
	e.Client = client
	e.Now = func() time.Time { return frozen }
	e.Logf = func(string, ...any) {}

	res := e.Run(context.Background(), RunParams{
		Peak:    "2026-08-12",
		Days:    0,
		Source:  SourceOpenMeteo,
		Sites:   redlineOneSite(),
		NoCache: true,
		Quiet:   true,
		OutDir:  t.TempDir(),
	})
	if res.ExitCode != 0 {
		t.Fatalf("双模型对比运行失败（exit=%d）：%v", res.ExitCode, res.Errors)
	}
	if len(res.Compare) == 0 {
		t.Fatal("双模型对比未产出任何对比行")
	}

	var iconOK, gfsNotOK, iconOnly int
	for _, r := range res.Compare {
		if r.IconRating == model.RATING_OK {
			iconOK++
		}
		if r.GfsRating != model.RATING_OK {
			gfsNotOK++
		}
		if r.Consensus == model.ConsensusIconOnly {
			iconOnly++
		}
		if r.Consensus == model.ConsensusBothOK {
			t.Fatalf("ICON 通透、GFS 不宜却被判 both_ok，共识逻辑错误：%s", r.TimeISO)
		}
	}
	if iconOK == 0 {
		t.Fatal("ICON 侧（通透报文）没有任何 ✅通透 行，配对或评级异常")
	}
	if gfsNotOK == 0 {
		t.Fatal("GFS 侧（阴云报文）没有任何非通透行，配对或评级异常")
	}
	if iconOnly == 0 {
		t.Fatal("ICON 通透而 GFS 不宜的时段应标 icon_only 分歧，却未出现")
	}
}
