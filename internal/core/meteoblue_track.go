package core

import (
	"context"
	"time"

	"github.com/prophetcro/astro-mountain/internal/model"
)

// MeteoblueFetcher 抽象 C 轨（Meteoblue）的取数能力。
//
// core 只认这个接口，实现由 api/meteoblue 提供并在组装时注入，
// 使内核不直接依赖具体的第三方客户端。
//
// 与 B 轨 TomorrowFetcher 的关键区别：Meteoblue 没有云底高度这类需要再装配的
// 中间产物，它自己产出维度与 A 轨完全兼容的 []model.HourRow（分层云量 + 降水 +
// 能见度评估），因此 FetchSite 直接返回 HourRow，主流程把它并入主 rows 即可，
// 报告层无需为 C 轨写任何独立渲染分支。Relation 填固定占位说明（Meteoblue 不反演云海几何）。
type MeteoblueFetcher interface {
	// Name 返回取数器名称，用于日志与提示。
	Name() string

	// FetchSite 拉取单个站点的逐小时评估行（Meteoblue 专有评估）。
	// start/end 限定时间窗，targetNights 限定观测夜；两者之外的时刻不产生行。
	// 未配置 key 时返回实现自定义的「无 key」错误，调用方据此中止而非回落其它源。
	FetchSite(ctx context.Context, site Site, start, end time.Time,
		targetNights map[string]bool) ([]model.HourRow, error)
}

// MeteoblueWired 报告是否已注入 C 轨取数器（含 nil 接收者保护）。
func (e *Engine) MeteoblueWired() bool {
	return e != nil && e.MeteoblueFetcher != nil
}

// MeteoblueDeliverable 报告 C 轨能否真正交付出报告。
// 目前等价于是否已接线，独立成方法是为了让「能取数」与「能交付」
// 将来可以分别演进。
func (e *Engine) MeteoblueDeliverable() bool {
	return e.MeteoblueWired()
}
