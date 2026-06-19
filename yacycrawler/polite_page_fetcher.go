package yacycrawler

import (
	"context"
	"errors"
	"fmt"
)

var ErrDisallowedByRobots = errors.New("disallowed by robots.txt")

type PolitePageFetcher struct {
	inner PageSource
	gate  *PolitenessGate
}

func NewPolitePageFetcher(inner PageSource, gate *PolitenessGate) *PolitePageFetcher {
	return &PolitePageFetcher{inner: inner, gate: gate}
}

func (f *PolitePageFetcher) Fetch(ctx context.Context, rawURL string) (FetchedPage, error) {
	allowed, err := f.gate.Allow(ctx, rawURL)
	if err != nil {
		return FetchedPage{}, err
	}
	if !allowed {
		return FetchedPage{}, ErrDisallowedByRobots
	}
	page, err := f.inner.Fetch(ctx, rawURL)
	if err != nil {
		return FetchedPage{}, fmt.Errorf("inner fetch: %w", err)
	}
	return page, nil
}
