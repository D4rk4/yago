package websearch

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func (s *FallbackSearcher) searchSupplement(
	ctx context.Context,
	request searchcore.Request,
	known map[string]struct{},
) parallelProviderOutcome {
	prepared := newProviderQueryForRequest(request)
	prepared.knownResults = known
	results, err := s.searchPreparedProvider(ctx, prepared, maxCachedResults)
	discovered := relevantWebResults(request, results)
	accepted := novelContribution(
		resultsMatchingConstraints(request, discovered),
		prepared.knownResults,
	)
	return parallelProviderOutcome{results: accepted, discovered: discovered, err: err}
}

func (s *FallbackSearcher) completeWebSearch(
	ctx context.Context,
	request searchcore.Request,
	primary searchcore.Response,
	provider parallelProviderOutcome,
) (searchcore.Response, error) {
	discovered := provider.discovered
	if discovered == nil {
		discovered = relevantWebResults(request, provider.results)
	}
	results := verifiedWebResults(request, provider.results)
	if provider.err != nil {
		primary = failedParallelProviderResponse(ctx, primary, provider.err)
	}
	if s.seeder != nil && len(discovered) > 0 {
		s.seedWebResults(ctx, discovered)
	}
	web := toCoreResults(results, request.Limit)
	if len(primary.Results) > 0 || len(web) > 0 {
		return mergeParallelResults(primary, web, request), nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return primary, fmt.Errorf("supplement search: %w", cause)
	}
	return primary, nil
}
