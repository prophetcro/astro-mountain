package tomorrow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(t time.Time) *fakeClock { return &fakeClock{t: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

var baseTime = time.Date(2026, 8, 6, 21, 0, 0, 0, time.FixedZone("CST", 8*3600))

func newTestLedger(t *testing.T, limits Limits) (*Ledger, *fakeClock, string) {
	t.Helper()
	dir := t.TempDir()
	clk := newClock(baseTime)
	l := NewLedger(dir, limits,
		WithLedgerClock(clk.Now),
		WithLedgerSleep(func(time.Duration) {}),
	)
	if l == nil {
		t.Fatal("NewLedger 在非空目录下返回了 nil")
	}
	return l, clk, dir
}

func testLimits() Limits {
	return Limits{PerHour: 5, PerDay: 10, MinInterval: 400 * time.Millisecond}
}

const siteCountPerRound = 13

func recordN(t *testing.T, l *Ledger, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := l.Record(EventOK, "测试点位"); err != nil {
			t.Fatalf("第 %d 次 Record 失败：%v", i+1, err)
		}
	}
}

func TestNilLedgerIsFullyNoOp(t *testing.T) {
	var l *Ledger

	if l.Active() {
		t.Error("nil 台账的 Active() 应为 false")
	}
	if l.Path() != "" {
		t.Errorf("nil 台账的 Path() 应为空串，实际 %q", l.Path())
	}
	if err := l.Record(EventOK, "x"); err != nil {
		t.Errorf("nil 台账 Record 应为 no-op，实际返回 %v", err)
	}
	if d := l.Throttle(); d != 0 {
		t.Errorf("nil 台账 Throttle 应为 0，实际 %v", d)
	}
	if _, err := l.Snapshot(); err != nil {
		t.Errorf("nil 台账 Snapshot 应无错误，实际 %v", err)
	}
	if d := l.Budget(9999); !d.Allowed {
		t.Error("nil 台账应放行任意请求数——它表示「未启用配额治理」")
	}
}

func TestNewLedgerWithEmptyDirReturnsNil(t *testing.T) {
	if l := NewLedger("", DefaultLimits()); l != nil {
		t.Error("空目录应返回 nil 台账（表示不启用配额治理）")
	}
}

func TestRecordPersistsAcrossLedgerInstances(t *testing.T) {
	l, clk, dir := newTestLedger(t, testLimits())
	recordN(t, l, 3)

	l2 := NewLedger(dir, testLimits(), WithLedgerClock(clk.Now))
	u, err := l2.Snapshot()
	if err != nil {
		t.Fatalf("重新打开台账失败：%v", err)
	}
	if u.UsedHour != 3 || u.UsedDay != 3 {
		t.Fatalf("跨实例读到用量 hour=%d day=%d，期望都是 3——"+
			"内存计数器活不过一次运行，这正是台账必须落盘的原因",
			u.UsedHour, u.UsedDay)
	}
}

func TestLedgerFileIsHumanReadable(t *testing.T) {
	l, _, _ := newTestLedger(t, testLimits())
	recordN(t, l, 2)

	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatalf("读取台账失败：%v", err)
	}
	text := string(data)

	for _, want := range []string{
		`"version": 1`,
		`"_note"`,
		`"per_hour": 5`,
		`"events"`,
		`"kind": "ok"`,
		"2026-08-06T21:00:00+08:00",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("台账缺少可读字段 %q，全文：\n%s", want, text)
		}
	}
	var probe map[string]any
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Errorf("台账不是合法 JSON：%v", err)
	}
}

func TestRateLimitedAndFailedStillCount(t *testing.T) {
	l, _, _ := newTestLedger(t, testLimits())

	if err := l.Record(EventRateLimited, "被限流的点位"); err != nil {
		t.Fatalf("Record 429 失败：%v", err)
	}
	if err := l.Record(EventFailed, "500 的点位"); err != nil {
		t.Fatalf("Record failed 失败：%v", err)
	}

	u, err := l.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot 失败：%v", err)
	}
	if u.UsedHour != 2 {
		t.Fatalf("UsedHour = %d，期望 2——429/失败不计数会让客户端以为还有额度，"+
			"而服务端那边早已扣过", u.UsedHour)
	}
}

func TestSlidingWindowExpiresHourlyButKeepsDaily(t *testing.T) {
	l, clk, _ := newTestLedger(t, testLimits())
	recordN(t, l, 4)

	clk.Advance(time.Hour - time.Second)
	u, _ := l.Snapshot()
	if u.UsedHour != 4 {
		t.Errorf("59'59\" 后 UsedHour = %d，期望仍是 4（窗口未满）", u.UsedHour)
	}

	clk.Advance(2 * time.Second)
	u, _ = l.Snapshot()
	if u.UsedHour != 0 {
		t.Errorf("满 1 小时后 UsedHour = %d，期望 0", u.UsedHour)
	}
	if u.UsedDay != 4 {
		t.Errorf("满 1 小时后 UsedDay = %d，期望仍是 4（天窗口没到）", u.UsedDay)
	}

	clk.Advance(24 * time.Hour)
	u, _ = l.Snapshot()
	if u.UsedHour != 0 || u.UsedDay != 0 {
		t.Errorf("满 24 小时后 hour=%d day=%d，期望都是 0", u.UsedHour, u.UsedDay)
	}
}

func TestWindowSlidesRatherThanBuckets(t *testing.T) {
	l, clk, _ := newTestLedger(t, testLimits())

	recordN(t, l, 2)
	clk.Advance(40 * time.Minute)
	recordN(t, l, 2)

	clk.Advance(25 * time.Minute)
	u, _ := l.Snapshot()
	if u.UsedHour != 2 {
		t.Fatalf("22:05 时 UsedHour = %d，期望 2（只剩 21:40 那两条）。"+
			"若为 4 说明窗口没在滑动；若为 0 说明整点桶被清空了", u.UsedHour)
	}
}

func TestOldEventsPrunedOnWrite(t *testing.T) {
	l, clk, _ := newTestLedger(t, testLimits())
	recordN(t, l, 3)

	clk.Advance(25 * time.Hour)
	recordN(t, l, 1)

	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatalf("读取台账失败：%v", err)
	}
	var f ledgerFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("解析台账失败：%v", err)
	}
	if len(f.Events) != 1 {
		t.Fatalf("裁剪后事件数 = %d，期望 1（3 条过期 + 1 条新的）", len(f.Events))
	}
}

func TestBudgetAllowsWhenEnough(t *testing.T) {
	l, _, _ := newTestLedger(t, testLimits())
	recordN(t, l, 2)

	d := l.Budget(3)
	if !d.Allowed {
		t.Fatalf("2 已用 + 3 需求 = 5 恰好等于上限 5，应当放行；实际拒绝：%s", d.Reason())
	}
	if d.Message() != "" {
		t.Errorf("放行时 Message 应为空，实际 %q", d.Message())
	}
	if got := d.Usage.RemainingHour(); got != 3 {
		t.Errorf("RemainingHour = %d，期望 3", got)
	}
}

func TestBudgetRejectsWholeRoundWhenShort(t *testing.T) {
	l, _, _ := newTestLedger(t, testLimits())
	recordN(t, l, 3)

	d := l.Budget(3)
	if d.Allowed {
		t.Fatal("3 已用 + 3 需求 = 6 超过上限 5，必须拒绝整轮——" +
			"「先跑两个再说」正是要禁止的半套数据")
	}
	if d.Window != "hour" {
		t.Errorf("Window = %q，期望 hour", d.Window)
	}
	if !errors.Is(d.Err, ErrQuotaExhausted) {
		t.Errorf("Err = %v，期望 ErrQuotaExhausted", d.Err)
	}
}

func TestBudgetIsAllOrNothing(t *testing.T) {
	l, _, _ := newTestLedger(t, testLimits())
	recordN(t, l, 4)

	d := l.Budget(13)
	if d.Allowed {
		t.Fatal("只剩 1 次额度却要跑 13 个点位，必须整轮拒绝")
	}

	u, _ := l.Snapshot()
	if u.UsedHour != 4 {
		t.Errorf("预检后 UsedHour = %d，期望仍是 4（预检不得消耗额度）", u.UsedHour)
	}
}

func TestBudgetReportsDayWindowFirst(t *testing.T) {

	l, clk, _ := newTestLedger(t, Limits{PerHour: 100, PerDay: 3})
	recordN(t, l, 2)
	clk.Advance(2 * time.Hour)

	d := l.Budget(2)
	if d.Allowed {
		t.Fatal("天窗口 2+2 > 3，应当拒绝")
	}
	if d.Window != "day" {
		t.Fatalf("Window = %q，期望 day——天窗口恢复要等到明天，先报它才不会误导用户", d.Window)
	}
}

func TestBudgetZeroNeedAlwaysAllowed(t *testing.T) {
	l, _, _ := newTestLedger(t, testLimits())
	recordN(t, l, 5)

	if d := l.Budget(0); !d.Allowed {
		t.Fatal("需求为 0（全部命中缓存）时必须放行——不需要额度就不该被额度挡住")
	}
}

func TestBudgetTreatsNonPositiveLimitAsUnlimited(t *testing.T) {
	l, _, _ := newTestLedger(t, Limits{PerHour: 0, PerDay: -1})
	recordN(t, l, 50)

	d := l.Budget(999)
	if !d.Allowed {
		t.Fatal("上限 <=0 表示不设限，应当放行")
	}
	if got := d.Usage.RemainingHour(); got != -1 {
		t.Errorf("不设限时 RemainingHour 应为 -1，实际 %d", got)
	}
}

func TestRecoverAtPointsToOldestExpiry(t *testing.T) {
	l, clk, _ := newTestLedger(t, Limits{PerHour: 3, PerDay: 100})

	recordN(t, l, 1)
	clk.Advance(10 * time.Minute)
	recordN(t, l, 1)
	clk.Advance(10 * time.Minute)
	recordN(t, l, 1)
	clk.Advance(10 * time.Minute)

	d := l.Budget(1)
	if d.Allowed {
		t.Fatal("已用满 3 条时不该放行")
	}
	want := baseTime.Add(time.Hour)
	if !d.RecoverAt.Equal(want) {
		t.Fatalf("RecoverAt = %s，期望 %s（最旧一条 21:00 在 22:00 滑出窗口）",
			d.RecoverAt.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if got := d.RecoverIn(); got != 30*time.Minute {
		t.Errorf("RecoverIn = %v，期望 30m", got)
	}
	if !strings.Contains(d.Message(), "30 分钟") {
		t.Errorf("提示里应有人话的恢复时长，实际：%s", d.Message())
	}
}

func TestRecoverAtEmptyWhenNeedExceedsLimit(t *testing.T) {
	l, _, _ := newTestLedger(t, Limits{PerHour: 5, PerDay: 100})
	d := l.Budget(13)
	if d.Allowed {
		t.Fatal("需求 13 超过小时上限 5，应当拒绝")
	}
	if !d.RecoverAt.IsZero() {
		t.Errorf("需求超上限时不该给恢复时刻，实际 %v", d.RecoverAt)
	}
}

func TestDegradeMessageIsSelfExplanatory(t *testing.T) {
	l, _, _ := newTestLedger(t, testLimits())
	recordN(t, l, 4)

	msg := l.Budget(3).Message()
	for _, want := range []string{
		"整体降级",
		"1 小时",
		"4/5",
		"半套",
		"Open-Meteo",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("降级提示缺少 %q，实际全文：\n%s", want, msg)
		}
	}
}

func TestCorruptLedgerDegradesConservatively(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"截断的 JSON", `{"version":1,"events":[{"at":`},
		{"空文件", ""},
		{"根本不是 JSON", "这不是 JSON，是某个编辑器留下的垃圾"},
		{"版本不认识", `{"version":99,"events":[]}`},
		{"事件缺时间戳", `{"version":1,"events":[{"kind":"ok"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, _, dir := newTestLedger(t, testLimits())
			if err := os.WriteFile(filepath.Join(dir, LedgerFileName),
				[]byte(tc.content), 0o644); err != nil {
				t.Fatalf("构造损坏台账失败：%v", err)
			}

			d := l.Budget(1)
			if d.Allowed {
				t.Fatal("台账损坏时必须保守拒绝——" +
					"按「没记录」处理会在「其实已用满」的情况下把账号打爆，" +
					"而按「用满」处理最坏只是白等一小时。代价不对称")
			}
			if !errors.Is(d.Err, ErrLedgerCorrupt) {
				t.Errorf("Err = %v，期望 ErrLedgerCorrupt", d.Err)
			}
			if !strings.Contains(d.Message(), "台账") {
				t.Errorf("损坏提示应说明是台账问题，实际：%s", d.Message())
			}
		})
	}
}

func TestMissingLedgerIsCleanStart(t *testing.T) {
	l, _, dir := newTestLedger(t, DefaultLimits())

	if _, err := os.Stat(filepath.Join(dir, LedgerFileName)); !os.IsNotExist(err) {
		t.Fatalf("前置条件不成立：台账文件不该存在，err=%v", err)
	}

	u, err := l.Snapshot()
	if err != nil {
		t.Fatalf("台账不存在时不该报错，实际 %v", err)
	}
	if u.UsedHour != 0 || u.UsedDay != 0 {
		t.Errorf("初始用量应为 0，实际 hour=%d day=%d", u.UsedHour, u.UsedDay)
	}

	d := l.Budget(siteCountPerRound)
	if !d.Allowed {
		t.Errorf("第一次运行必须能跑满一轮，实际被拒：%s", d.Reason())
	}

	if _, err := os.Stat(filepath.Join(dir, LedgerFileName)); !os.IsNotExist(err) {
		t.Errorf("预检不该创建台账文件，err=%v", err)
	}
}

func TestFreeTierFitsExactlyOneRoundPerHour(t *testing.T) {
	l, _, _ := newTestLedger(t, DefaultLimits())

	if d := l.Budget(siteCountPerRound); !d.Allowed {
		t.Fatalf("第一轮应放行，实际：%s", d.Reason())
	}
	recordN(t, l, siteCountPerRound)

	d := l.Budget(siteCountPerRound)
	if d.Allowed {
		t.Fatalf("同一小时内第二轮应被拒（%d+%d>%d）",
			siteCountPerRound, siteCountPerRound, DefaultPerHour)
	}
	if d.Window != "hour" {
		t.Errorf("应命中小时窗口，实际 %q", d.Window)
	}
	if d.RecoverAt.IsZero() {
		t.Error("小时窗口受限时应给出恢复时间")
	}
}

func TestBrokenFlagIsSticky(t *testing.T) {
	l, _, dir := newTestLedger(t, testLimits())
	path := filepath.Join(dir, LedgerFileName)
	if err := os.WriteFile(path, []byte("坏文件"), 0o644); err != nil {
		t.Fatalf("构造损坏台账失败：%v", err)
	}
	if l.Budget(1).Allowed {
		t.Fatal("损坏时应拒绝")
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("删除损坏台账失败：%v", err)
	}
	if l.Budget(1).Allowed {
		t.Fatal("粘性故障标记失效：同一进程内修好文件就放行，" +
			"等于给「写失败导致漏记」开了后门")
	}
}

func TestThrottleWaitsRemainingInterval(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(baseTime)
	var slept []time.Duration
	l := NewLedger(dir, Limits{PerHour: 100, PerDay: 100, MinInterval: 400 * time.Millisecond},
		WithLedgerClock(clk.Now),
		WithLedgerSleep(func(d time.Duration) { slept = append(slept, d) }),
	)

	if got := l.Throttle(); got != 0 {
		t.Errorf("首次 Throttle = %v，期望 0", got)
	}
	recordN(t, l, 1)

	clk.Advance(150 * time.Millisecond)
	if got := l.Throttle(); got != 250*time.Millisecond {
		t.Errorf("Throttle = %v，期望 250ms", got)
	}
	if len(slept) != 1 || slept[0] != 250*time.Millisecond {
		t.Errorf("实际睡眠记录 = %v，期望 [250ms]", slept)
	}

	clk.Advance(500 * time.Millisecond)
	if got := l.Throttle(); got != 0 {
		t.Errorf("间隔已满时 Throttle = %v，期望 0", got)
	}
}

func TestThrottleDisabledWhenIntervalZero(t *testing.T) {
	dir := t.TempDir()
	clk := newClock(baseTime)
	l := NewLedger(dir, Limits{PerHour: 100, PerDay: 100, MinInterval: 0},
		WithLedgerClock(clk.Now), WithLedgerSleep(func(time.Duration) {}))
	recordN(t, l, 1)
	if got := l.Throttle(); got != 0 {
		t.Errorf("间隔为 0 时不该节流，实际 %v", got)
	}
}

func TestDefaultLimitsMatchMeasuredFreeTier(t *testing.T) {
	l := DefaultLimits()
	if l.PerHour != 25 {
		t.Errorf("PerHour = %d，期望实测值 25", l.PerHour)
	}
	if l.PerDay != 500 {
		t.Errorf("PerDay = %d，期望实测值 500", l.PerDay)
	}
	if l.MinInterval != 400*time.Millisecond {
		t.Errorf("MinInterval = %v，期望 400ms（3 req/s 留余量）", l.MinInterval)
	}

	if 2*13 <= l.PerHour {
		t.Error("按当前限额两轮完整报告不再超限，配额治理的前提假设已变，" +
			"请重新评估缓存 TTL 与降级文案")
	}
}

func TestConcurrentRecordWithinProcess(t *testing.T) {
	dir := t.TempDir()

	l := NewLedger(dir, Limits{PerHour: 1000, PerDay: 1000})

	const n = 30
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Record(EventOK, "并发"); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("并发 Record 失败：%v", err)
	}

	u, err := l.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot 失败：%v", err)
	}
	if u.UsedHour != n {
		t.Fatalf("并发记账后 UsedHour = %d，期望 %d——丢记录意味着额度会被重复花掉",
			u.UsedHour, n)
	}
}

func TestConcurrentRecordAcrossLedgerInstances(t *testing.T) {
	dir := t.TempDir()
	const instances = 6
	const perInstance = 5

	var wg sync.WaitGroup
	errCh := make(chan error, instances*perInstance)
	for i := 0; i < instances; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			l := NewLedger(dir, Limits{PerHour: 1000, PerDay: 1000})
			for j := 0; j < perInstance; j++ {
				if err := l.Record(EventOK, "跨实例"); err != nil {
					errCh <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("跨实例 Record 失败：%v", err)
	}

	u, err := NewLedger(dir, Limits{PerHour: 1000, PerDay: 1000}).Snapshot()
	if err != nil {
		t.Fatalf("Snapshot 失败：%v", err)
	}
	if want := instances * perInstance; u.UsedHour != want {
		t.Fatalf("跨实例并发后 UsedHour = %d，期望 %d——"+
			"读-改-写没有在锁内完成时，后写的会覆盖先写的", u.UsedHour, want)
	}
}

func TestStaleLockIsBroken(t *testing.T) {
	dir := t.TempDir()
	l := NewLedger(dir, testLimits())

	lockPath := filepath.Join(dir, ledgerLockName)
	if err := os.WriteFile(lockPath, []byte("pid=99999 崩溃残留\n"), 0o644); err != nil {
		t.Fatalf("构造残留锁失败：%v", err)
	}
	old := time.Now().Add(-2 * lockStaleAfter)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatalf("回拨锁文件时间失败：%v", err)
	}

	if err := l.Record(EventOK, "夺锁后写入"); err != nil {
		t.Fatalf("残留锁应被夺取，实际 Record 失败：%v", err)
	}
	u, _ := l.Snapshot()
	if u.UsedHour != 1 {
		t.Errorf("UsedHour = %d，期望 1", u.UsedHour)
	}
}

func TestFutureTimestampsCountConservatively(t *testing.T) {
	l, clk, _ := newTestLedger(t, testLimits())
	recordN(t, l, 3)

	clk.Advance(-2 * time.Hour)

	u, err := l.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot 失败：%v", err)
	}
	if u.UsedHour != 3 || u.UsedDay != 3 {
		t.Fatalf("时钟回拨后 hour=%d day=%d，期望都是 3——"+
			"把未来时间戳当成不存在，等于时钟一乱额度就无限", u.UsedHour, u.UsedDay)
	}
}
