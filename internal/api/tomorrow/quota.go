package tomorrow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DefaultPerHour = 25

	DefaultPerDay = 500

	DefaultMinIntervalMS = 400
)

const LedgerFileName = "tomorrow-quota.json"

const ledgerLockName = LedgerFileName + ".lock"

const ledgerSchemaVersion = 1

const maxLedgerEvents = 2000

const (
	lockPollInterval = 15 * time.Millisecond
	lockMaxWait      = 5 * time.Second

	lockStaleAfter = 20 * time.Second
)

const (
	windowHour = time.Hour
	windowDay  = 24 * time.Hour
)

var (
	ErrQuotaExhausted = errors.New("tomorrow: 配额不足，本轮已整体降级")

	ErrLedgerCorrupt = errors.New("tomorrow: 配额台账损坏，已保守视为额度用尽")

	ErrLedgerLocked = errors.New("tomorrow: 配额台账被其他进程长时间占用")
)

type EventKind string

const (
	EventOK EventKind = "ok"

	EventRateLimited EventKind = "rate_limited"

	EventFailed EventKind = "failed"
)

type ledgerEvent struct {
	At   time.Time `json:"at"`
	Kind EventKind `json:"kind"`

	Note string `json:"note,omitempty"`
}

type ledgerFile struct {
	Version int    `json:"version"`
	Note    string `json:"_note"`
	Limits  struct {
		PerHour       int `json:"per_hour"`
		PerDay        int `json:"per_day"`
		MinIntervalMS int `json:"min_interval_ms"`
	} `json:"limits"`
	Events []ledgerEvent `json:"events"`
}

const ledgerNote = "Tomorrow.io 真实请求台账（滑动窗口配额治理用）。" +
	"events 按时间升序，只保留最近 24 小时。删除本文件会让程序保守地" +
	"认为额度已用尽，直到窗口自然过去——想强制重置请删掉后等一小时，" +
	"或改小 config 里的 api.tomorrow_quota_* 限额。"

type Limits struct {
	PerHour     int
	PerDay      int
	MinInterval time.Duration
}

func DefaultLimits() Limits {
	return Limits{
		PerHour:     DefaultPerHour,
		PerDay:      DefaultPerDay,
		MinInterval: DefaultMinIntervalMS * time.Millisecond,
	}
}

func (l Limits) normalized() Limits {
	if l.MinInterval < 0 {
		l.MinInterval = 0
	}
	return l
}

type Ledger struct {
	dir    string
	limits Limits

	now   func() time.Time
	sleep func(time.Duration)

	mu sync.Mutex

	broken error
}

type LedgerOption func(*Ledger)

func WithLedgerClock(now func() time.Time) LedgerOption {
	return func(l *Ledger) {
		if now != nil {
			l.now = now
		}
	}
}

func WithLedgerSleep(fn func(time.Duration)) LedgerOption {
	return func(l *Ledger) {
		if fn != nil {
			l.sleep = fn
		}
	}
}

func NewLedger(dir string, limits Limits, opts ...LedgerOption) *Ledger {
	if dir == "" {
		return nil
	}
	l := &Ledger{
		dir:    dir,
		limits: limits.normalized(),
		now:    time.Now,
		sleep:  time.Sleep,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func (l *Ledger) Limits() Limits {
	if l == nil {
		return Limits{}
	}
	return l.limits
}

func (l *Ledger) Path() string {
	if l == nil {
		return ""
	}
	return filepath.Join(l.dir, LedgerFileName)
}

func (l *Ledger) Active() bool { return l != nil }

type Usage struct {
	Now time.Time

	UsedHour int
	UsedDay  int

	Limits Limits

	LastAt time.Time

	hourEvents []time.Time

	dayEvents []time.Time
}

func (u Usage) RemainingHour() int {
	if u.Limits.PerHour <= 0 {
		return -1
	}
	if r := u.Limits.PerHour - u.UsedHour; r > 0 {
		return r
	}
	return 0
}

func (u Usage) RemainingDay() int {
	if u.Limits.PerDay <= 0 {
		return -1
	}
	if r := u.Limits.PerDay - u.UsedDay; r > 0 {
		return r
	}
	return 0
}

func (l *Ledger) Snapshot() (Usage, error) {
	if l == nil {
		return Usage{Now: time.Now()}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshotLocked()
}

func (l *Ledger) snapshotLocked() (Usage, error) {
	now := l.now()
	u := Usage{Now: now, Limits: l.limits}
	if l.broken != nil {
		return u, l.broken
	}
	events, err := l.load()
	if err != nil {
		l.broken = err
		return u, err
	}
	for _, e := range events {
		age := now.Sub(e.At)

		if age < 0 {
			u.UsedHour++
			u.UsedDay++
			u.hourEvents = append(u.hourEvents, e.At)
			u.dayEvents = append(u.dayEvents, e.At)
			continue
		}
		if age < windowDay {
			u.UsedDay++
			u.dayEvents = append(u.dayEvents, e.At)
		}
		if age < windowHour {
			u.UsedHour++
			u.hourEvents = append(u.hourEvents, e.At)
		}
	}
	if n := len(events); n > 0 {
		u.LastAt = events[n-1].At
	}
	return u, nil
}

func (l *Ledger) Record(kind EventKind, note string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.appendLocked(ledgerEvent{At: l.now(), Kind: kind, Note: note}); err != nil {

		l.broken = fmt.Errorf("%w：%v", ErrLedgerCorrupt, err)
		return l.broken
	}
	return nil
}

func (l *Ledger) appendLocked(ev ledgerEvent) error {
	release, err := l.acquireLock()
	if err != nil {
		return err
	}
	defer release()

	events, err := l.load()
	if err != nil {
		return err
	}
	events = append(events, ev)
	return l.store(events, l.now())
}

func (l *Ledger) Throttle() time.Duration {
	if l == nil || l.limits.MinInterval <= 0 {
		return 0
	}
	l.mu.Lock()
	u, err := l.snapshotLocked()
	l.mu.Unlock()
	if err != nil || u.LastAt.IsZero() {
		return 0
	}
	wait := l.limits.MinInterval - u.Now.Sub(u.LastAt)
	if wait <= 0 {
		return 0
	}
	l.sleep(wait)
	return wait
}

func (l *Ledger) load() ([]ledgerEvent, error) {
	data, err := os.ReadFile(l.Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w（读取 %s 失败：%v）", ErrLedgerCorrupt, l.Path(), err)
	}
	if len(data) == 0 {

		return nil, fmt.Errorf("%w（%s 是空文件）", ErrLedgerCorrupt, l.Path())
	}
	var f ledgerFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%w（%s 解析失败：%v）", ErrLedgerCorrupt, l.Path(), err)
	}
	if f.Version != ledgerSchemaVersion {
		return nil, fmt.Errorf("%w（%s 版本 %d 无法识别，本程序只认 %d）",
			ErrLedgerCorrupt, l.Path(), f.Version, ledgerSchemaVersion)
	}
	for i, e := range f.Events {
		if e.At.IsZero() {
			return nil, fmt.Errorf("%w（%s 第 %d 条记录缺少时间戳）",
				ErrLedgerCorrupt, l.Path(), i+1)
		}
	}
	events := f.Events
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	return events, nil
}

func (l *Ledger) store(events []ledgerEvent, now time.Time) error {
	kept := pruneEvents(events, now)

	var f ledgerFile
	f.Version = ledgerSchemaVersion
	f.Note = ledgerNote
	f.Limits.PerHour = l.limits.PerHour
	f.Limits.PerDay = l.limits.PerDay
	f.Limits.MinIntervalMS = int(l.limits.MinInterval / time.Millisecond)
	f.Events = kept

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配额台账失败：%w", err)
	}
	data = append(data, '\n')

	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return fmt.Errorf("创建台账目录 %s 失败：%w", l.dir, err)
	}
	tmp, err := os.CreateTemp(l.dir, LedgerFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("创建台账临时文件失败：%w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("写台账临时文件失败：%w", err)
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("同步台账临时文件失败：%w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("关闭台账临时文件失败：%w", err)
	}
	if err := os.Rename(tmpName, l.Path()); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("落盘台账 %s 失败：%w", l.Path(), err)
	}
	return nil
}

func pruneEvents(events []ledgerEvent, now time.Time) []ledgerEvent {
	cutoff := now.Add(-windowDay)
	kept := make([]ledgerEvent, 0, len(events))
	for _, e := range events {
		if e.At.After(cutoff) {
			kept = append(kept, e)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].At.Before(kept[j].At) })
	if len(kept) > maxLedgerEvents {

		kept = kept[len(kept)-maxLedgerEvents:]
	}
	return kept
}

func (l *Ledger) acquireLock() (func(), error) {
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建台账目录 %s 失败：%w", l.dir, err)
	}
	lockPath := filepath.Join(l.dir, ledgerLockName)
	deadline := time.Now().Add(lockMaxWait)

	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {

			fmt.Fprintf(f, "pid=%d at=%s\n", os.Getpid(), time.Now().Format(time.RFC3339Nano))
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("获取台账锁 %s 失败：%w", lockPath, err)
		}
		if info, serr := os.Stat(lockPath); serr == nil {
			if time.Since(info.ModTime()) > lockStaleAfter {
				_ = os.Remove(lockPath)
				continue
			}
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("%w（%s，已等待 %s）", ErrLedgerLocked, lockPath, lockMaxWait)
		}
		time.Sleep(lockPollInterval)
	}
}

type Decision struct {
	Allowed bool

	Need int

	Usage Usage

	Window string

	RecoverAt time.Time

	Err error
}

func (d Decision) RecoverIn() time.Duration {
	if d.RecoverAt.IsZero() || d.Usage.Now.IsZero() {
		return 0
	}
	left := d.RecoverAt.Sub(d.Usage.Now)
	if left <= 0 {
		return 0
	}
	return left.Round(time.Second)
}

func (d Decision) Reason() string {
	switch {
	case d.Allowed:
		return ""
	case d.Err != nil && isCorrupt(d.Err):
		return "配额台账不可读，已保守视为额度用尽（宁可少跑一轮，也不能把账号打爆）"
	case d.Window == "day":

		return fmt.Sprintf("本轮需 %d 次请求，但 24 小时配额窗口只剩 %d 次",
			d.Need, clampNonNeg(d.Usage.RemainingDay()))
	case d.Window == "hour":
		return fmt.Sprintf("本轮需 %d 次请求，但 1 小时配额窗口只剩 %d 次",
			d.Need, clampNonNeg(d.Usage.RemainingHour()))
	default:
		return "配额预检未通过"
	}
}

func (d Decision) Message() string {
	if d.Allowed {
		return ""
	}
	var b strings.Builder
	b.WriteString("⚠ Tomorrow.io 轨道本轮整体降级：")
	b.WriteString(d.Reason())
	b.WriteString("。")

	if in := d.RecoverIn(); in > 0 {
		b.WriteString(fmt.Sprintf("预计 %s 后（约 %s）恢复。",
			humanDuration(in), d.RecoverAt.Format("15:04")))
	} else if d.Err != nil && isCorrupt(d.Err) {
		b.WriteString("请删除配额台账文件后重试；若刚跑过一轮，建议等满 1 小时再删，避免重复消耗。")
	}

	b.WriteString(fmt.Sprintf("（用量：1 小时 %s，24 小时 %s）",
		usageFrac(d.Usage.UsedHour, d.Usage.Limits.PerHour),
		usageFrac(d.Usage.UsedDay, d.Usage.Limits.PerDay)))
	b.WriteString("本轮不发起任何 Tomorrow.io 请求——半套双轨数据会让人误以为对比是完整的，" +
		"比没有第二轨更危险。Open-Meteo 轨道不受影响，结论照常给出。")
	return b.String()
}

func usageFrac(used, limit int) string {
	if limit <= 0 {
		return fmt.Sprintf("%d/∞", used)
	}
	return fmt.Sprintf("%d/%d", used, limit)
}

func clampNonNeg(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d.Seconds()+0.5))
	}
	total := int(d.Round(time.Minute) / time.Minute)
	h, m := total/60, total%60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%d 小时 %d 分钟", h, m)
	case h > 0:
		return fmt.Sprintf("%d 小时", h)
	default:
		return fmt.Sprintf("%d 分钟", m)
	}
}

func isCorrupt(err error) bool {
	return errors.Is(err, ErrLedgerCorrupt) || errors.Is(err, ErrLedgerLocked)
}

func (l *Ledger) Budget(need int) Decision {
	d := Decision{Need: need}
	if need <= 0 {
		d.Allowed = true
		d.Usage, _ = l.Snapshot()
		return d
	}
	if l == nil {
		d.Allowed = true
		d.Usage = Usage{Now: time.Now()}
		return d
	}

	u, err := l.Snapshot()
	d.Usage = u
	if err != nil {

		d.Err = err
		return d
	}

	if u.Limits.PerDay > 0 && u.UsedDay+need > u.Limits.PerDay {
		d.Window = "day"
		d.Err = ErrQuotaExhausted
		d.RecoverAt = recoverAt(u.dayEvents, u.Limits.PerDay, need, windowDay)
		return d
	}
	if u.Limits.PerHour > 0 && u.UsedHour+need > u.Limits.PerHour {
		d.Window = "hour"
		d.Err = ErrQuotaExhausted
		d.RecoverAt = recoverAt(u.hourEvents, u.Limits.PerHour, need, windowHour)
		return d
	}
	d.Allowed = true
	return d
}

func recoverAt(events []time.Time, limit, need int, window time.Duration) time.Time {
	c := len(events)
	k := c + need - limit
	if k <= 0 {
		return time.Time{}
	}
	if k > c {
		return time.Time{}
	}
	return events[k-1].Add(window)
}
