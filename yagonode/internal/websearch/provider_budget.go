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
	cacheIdentity string
	safeSearch    string
	acceptResults func([]Result) []Result
	knownResults  map[string]struct{}
	primary       *primaryResultSnapshot
}

type providerQuerySearcher interface {
	searchProviderQuery(context.Context, providerQuery, int) ([]Result, error)
}

func newProviderQuery(query string) providerQuery {
	prepared := providerQuery{
		submittedText: strings.TrimSpace(query),
		outboundText:  searchcore.NormalizeTextQuery(query),
	}
	prepared.cacheIdentity = prepared.outboundText

	return prepared
}

func WithProviderBudget(budget time.Duration) Option {
	return func(searcher *FallbackSearcher) { searcher.providerBudget = budget }
}

func WithProviderReserve(reserve time.Duration) Option {
	return func(searcher *FallbackSearcher) { searcher.providerReserve = reserve }
}

func (s *FallbackSearcher) searchPreparedProvider(
	ctx context.Context,
	preparedQuery providerQuery,
	limit int,
) ([]Result, error) {
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
	deadline := time.Now().Add(s.providerBudget)
	if parent, bounded := ctx.Deadline(); bounded &&
		parent.Add(-s.providerReserve).Before(deadline) {
		deadline = parent.Add(-s.providerReserve)
	}
	providerContext, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	results, err := search(providerContext)

	return providerResults(results, err)
}

func providerResults(results []Result, err error) ([]Result, error) {
	if err != nil {
		return results, fmt.Errorf("search web provider: %w", err)
	}

	return results, nil
}
