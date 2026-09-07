package websearch

import "context"

type primaryResultSnapshot struct {
	ready chan struct{}
	known map[string]struct{}
}

func (query providerQuery) known(ctx context.Context) map[string]struct{} {
	if query.primary == nil {
		return query.knownResults
	}
	select {
	case <-query.primary.ready:
		return query.primary.known
	case <-ctx.Done():
		return nil
	}
}

func novelContribution(results []Result, known map[string]struct{}) []Result {
	if len(withoutKnownResults(results, known)) == 0 {
		return nil
	}
	return withoutKnownResults(results, nil)
}
