package websearch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type providerQuery struct {
	submittedText string
	outboundText  string
}

type providerQuerySearcher interface {
	searchProviderQuery(context.Context, providerQuery, int) ([]Result, error)
}

func newProviderQuery(query string) providerQuery {
	return providerQuery{
		submittedText: strings.TrimSpace(query),
		outboundText:  searchcore.NormalizeTextQuery(query),
	}
}

func WithProviderBudget(budget time.Duration) Option {
	return func(searcher *FallbackSearcher) { searcher.providerBudget = budget }
}

func (s *FallbackSearcher) searchProvider(
	ctx context.Context,
	query string,
	limit int,
) ([]Result, error) {
	preparedQuery := newProviderQuery(query)
	search := func(searchContext context.Context) ([]Result, error) {
		if provider, ok := s.provider.(providerQuerySearcher); ok {
			return provider.searchProviderQuery(searchContext, preparedQuery, limit)
		}

		return s.provider.Search(searchContext, preparedQuery.outboundText, limit)
	}
	if s.providerBudget <= 0 {
		results, err := search(ctx)

		return providerResults(results, err)
	}
	providerContext, cancel := context.WithTimeout(ctx, s.providerBudget)
	defer cancel()
	results, err := search(providerContext)

	return providerResults(results, err)
}

func providerResults(results []Result, err error) ([]Result, error) {
	if err != nil {
		return nil, fmt.Errorf("search web provider: %w", err)
	}

	return results, nil
}
