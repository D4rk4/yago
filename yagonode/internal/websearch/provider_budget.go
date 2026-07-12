package websearch

import (
	"context"
	"fmt"
	"time"
)

func WithProviderBudget(budget time.Duration) Option {
	return func(searcher *FallbackSearcher) { searcher.providerBudget = budget }
}

func (s *FallbackSearcher) searchProvider(
	ctx context.Context,
	query string,
	limit int,
) ([]Result, error) {
	if s.providerBudget <= 0 {
		results, err := s.provider.Search(ctx, query, limit)

		return providerResults(results, err)
	}
	providerContext, cancel := context.WithTimeout(ctx, s.providerBudget)
	defer cancel()
	results, err := s.provider.Search(providerContext, query, limit)

	return providerResults(results, err)
}

func providerResults(results []Result, err error) ([]Result, error) {
	if err != nil {
		return nil, fmt.Errorf("search web provider: %w", err)
	}

	return results, nil
}
