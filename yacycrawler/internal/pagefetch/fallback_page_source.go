package pagefetch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

type FallbackPageSource struct {
	primary  PageSource
	fallback PageSource
}

func NewFallbackPageSource(primary, fallback PageSource) *FallbackPageSource {
	return &FallbackPageSource{primary: primary, fallback: fallback}
}

func (s *FallbackPageSource) Fetch(
	ctx context.Context,
	target *url.URL,
) (FetchedPage, error) {
	page, err := s.primary.Fetch(ctx, target)
	if err == nil {
		return page, nil
	}
	if !errors.Is(err, ErrPageRejected) {
		return FetchedPage{}, fmt.Errorf("primary fetch: %w", err)
	}

	page, err = s.fallback.Fetch(ctx, target)
	if err != nil {
		return FetchedPage{}, fmt.Errorf("fallback fetch: %w", err)
	}
	return page, nil
}
