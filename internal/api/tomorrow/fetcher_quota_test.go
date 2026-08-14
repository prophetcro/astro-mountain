package tomorrow

import (
	"testing"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/core"
)

func TestFetcherReportsQuotaRecovery(t *testing.T) {

	var (
		_ core.TomorrowFetcher       = (*Fetcher)(nil)
		_ core.TomorrowQuotaReporter = (*Fetcher)(nil)
	)

	var nilFetcher *Fetcher
	if got := nilFetcher.QuotaRecoverAt(); !got.IsZero() {
		t.Errorf("nil Fetcher 的 QuotaRecoverAt() = %v，期望零值", got)
	}
	if got := (&Fetcher{}).QuotaRecoverAt(); !got.IsZero() {
		t.Errorf("Client 为 nil 时 QuotaRecoverAt() = %v，期望零值", got)
	}

	f := NewFetcher(config.Default().API, false)
	f.Client.Quota = nil
	if got := f.QuotaRecoverAt(); !got.IsZero() {
		t.Errorf("未启用配额台账时 QuotaRecoverAt() = %v，期望零值（"+
			"没有台账就不知道用了多少，给不出恢复时间才是诚实的）", got)
	}
}

func TestQuotaRecoverAtDoesNotConsumeQuota(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 12, 10, 0, 0, 0, time.UTC)

	ledger := NewLedger(dir+"/quota.json", DefaultLimits(),
		WithLedgerClock(func() time.Time { return now }))

	f := NewFetcher(config.Default().API, false)
	f.Client.Quota = ledger

	before, err := ledger.Snapshot()
	if err != nil {
		t.Fatalf("读取台账快照失败：%v", err)
	}

	for i := 0; i < 5; i++ {
		_ = f.QuotaRecoverAt()
	}

	after, err := ledger.Snapshot()
	if err != nil {
		t.Fatalf("读取台账快照失败：%v", err)
	}
	if after.UsedDay != before.UsedDay || after.UsedHour != before.UsedHour {
		t.Errorf("问了 5 次恢复时间之后用量变了：日 %d→%d，时 %d→%d。\n"+
			"QuotaRecoverAt 必须是只读的——它在配额耗尽后被调用，"+
			"每问一次记一笔会让用户越等越久，而且这种多算要到下一轮才显形。",
			before.UsedDay, after.UsedDay, before.UsedHour, after.UsedHour)
	}
}
