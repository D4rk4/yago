package politeness

import (
	"context"
	"errors"
	"fmt"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
)

var ErrDisallowedByRobots = errors.New("disallowed by robots.txt")

type PolitePageFetcher struct {
	inner pagefetch.PageSource
	gate  *PolitenessGate
}

func NewPolitePageFetcher(inner pagefetch.PageSource, gate *PolitenessGate) *PolitePageFetcher {
	return &PolitePageFetcher{inner: inner, gate: gate}
}

func (f *PolitePageFetcher) Fetch(
	ctx context.Context,
	rawURL string,
) (pagefetch.FetchedPage, error) {
	allowed, err := f.gate.Allow(ctx, rawURL)
	if err != nil {
		return pagefetch.FetchedPage{}, err
	}
	if !allowed {
		return pagefetch.FetchedPage{}, ErrDisallowedByRobots
	}
	page, err := f.inner.Fetch(ctx, rawURL)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("inner fetch: %w", err)
	}
	return page, nil
}
