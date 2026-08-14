package tomorrow

import (
	"context"
	"errors"
	"time"

	"github.com/prophetcro/astro-mountain/internal/config"
	"github.com/prophetcro/astro-mountain/internal/dualtrack"
	"github.com/prophetcro/astro-mountain/internal/model"
)

type Fetcher struct {
	Client *Client
}

func NewFetcher(cfg config.APIConfig, useCache bool, opts ...Option) *Fetcher {
	return &Fetcher{Client: New(cfg, useCache, opts...)}
}

func (f *Fetcher) Name() string { return "Tomorrow.io" }

func (f *Fetcher) FetchSite(ctx context.Context, site model.Site) (
	samples []dualtrack.HourInput, datum string, quotaOK bool, err error) {

	if f == nil || f.Client == nil {

		return nil, "", true, errors.New("tomorrow: 取数器未正确构造（Client 为 nil）")
	}

	datum = string(f.Client.Datum)

	if d := f.Client.Quota.Budget(1); !d.Allowed {
		return nil, datum, false, nil
	}

	sr, ferr := f.Client.FetchSite(ctx, site)
	if ferr != nil {

		if errors.Is(ferr, ErrQuotaExhausted) {
			return nil, datum, false, nil
		}
		return nil, datum, true, ferr
	}

	inputs, cerr := ToDualTrackInputs(sr)
	if cerr != nil {
		return nil, datum, true, cerr
	}
	return inputs, datum, true, nil
}

func (f *Fetcher) QuotaRecoverAt() time.Time {
	if f == nil || f.Client == nil {
		return time.Time{}
	}
	return f.Client.Quota.Budget(1).RecoverAt
}
