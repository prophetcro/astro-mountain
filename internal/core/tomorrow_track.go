package core

import (
	"context"
	"time"

	"github.com/prophetcro/astro-mountain/internal/dualtrack"
)

// TomorrowFetcher 抽象 B 轨（Tomorrow.io）的取数能力。
//
// core 只认这个接口，实现由 api/tomorrow 提供并在组装时注入，
// 使内核不直接依赖具体的第三方客户端。
type TomorrowFetcher interface {
	// Name 返回取数器名称，用于日志与提示。
	Name() string

	// FetchSite 拉取单个站点的逐小时样本。
	// datum 说明云底高度的基准（离地或海拔，取值见 dualtrack.Datum*）；
	// quotaOK 为 false 表示配额已耗尽、样本不完整，调用方应据此降级而非报错。
	FetchSite(ctx context.Context, site Site) (
		samples []dualtrack.HourInput, datum string, quotaOK bool, err error)
}

// TomorrowWired 报告是否已注入 B 轨取数器（含 nil 接收者保护）。
func (e *Engine) TomorrowWired() bool {
	return e != nil && e.TomorrowFetcher != nil
}

// TomorrowDeliverable 报告 B 轨能否真正交付出报告。
// 目前等价于是否已接线，独立成方法是为了让「能取数」与「能交付」
// 将来可以分别演进。
func (e *Engine) TomorrowDeliverable() bool {
	return e.TomorrowWired()
}

// TomorrowQuotaReporter 是 TomorrowFetcher 的可选扩展：
// 实现它的取数器能报出配额恢复时刻，供报告提示「几点以后可以再试」。
type TomorrowQuotaReporter interface {
	QuotaRecoverAt() time.Time
}

// tomorrowNextAvailable 尽力取出配额恢复时刻；
// 取数器没实现该接口、或时刻为零值时返回 nil（表示未知，不做承诺）。
func tomorrowNextAvailable(f TomorrowFetcher) *time.Time {
	r, ok := f.(TomorrowQuotaReporter)
	if !ok {
		return nil
	}
	at := r.QuotaRecoverAt()
	if at.IsZero() {
		return nil
	}
	return &at
}

// runTomorrowTrack 跑单个站点的 B 轨：取数后与 A 轨的 rows 对齐装配，
// 返回装配结果与一条可选的警告文案（无警告时为空串）。
//
// 取数失败不放弃整站：仍以「无样本 + 配额异常」装配一份结果，
// 让报告显式呈现 B 轨缺数据，而不是悄悄退回 A 轨结论。
// 只有装配本身也失败时才返回 nil 结果。
func (e *Engine) runTomorrowTrack(ctx context.Context, site Site,
	rows []HourRow, utcOffsetHours float64) (*dualtrack.TrackResult, string) {

	samples, datum, quotaOK, err := e.TomorrowFetcher.FetchSite(ctx, site)
	if err != nil {

		res, aerr := dualtrack.Assemble(site.Name, utcOffsetHours, site.Alt,
			rows, nil, true, dualtrack.DatumAGL, &e.Cfg.Thresh)
		if aerr != nil {

			return nil, "[" + site.Name + "] B 轨装配失败：" + aerr.Error()
		}
		return res, "[" + site.Name + "] B 轨取数失败：" + err.Error()
	}

	res, aerr := dualtrack.Assemble(site.Name, utcOffsetHours, site.Alt,
		rows, samples, quotaOK, datum, &e.Cfg.Thresh)
	if aerr != nil {

		return nil, "[" + site.Name + "] B 轨不可用：" + aerr.Error()
	}

	if res.QuotaExhausted {
		res.NextAvailable = tomorrowNextAvailable(e.TomorrowFetcher)
	}
	return res, ""
}
