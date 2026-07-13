package websearch

import (
	"context"
	"log/slog"
	"net/url"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	msgFallbackFailed = "web-search fallback provider failed"
	webResultDecay    = 0.01
)

type FallbackSearcher struct {
	primary        searchcore.Searcher
	provider       Provider
	permit         func(searchcore.Request) bool
	seeder         CrawlSeeder
	providerBudget time.Duration
	spawnSeedWork  func(func()) bool
}

func NewFallbackSearcher(
	primary searchcore.Searcher,
	provider Provider,
	permit func(searchcore.Request) bool,
	opts ...Option,
) *FallbackSearcher {
	searcher := &FallbackSearcher{primary: primary, provider: provider, permit: permit}
	for _, opt := range opts {
		opt(searcher)
	}
	if searcher.seeder != nil {
		searcher.spawnSeedWork = webSeedProcessAdmission.try
	}

	return searcher
}

func (s *FallbackSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	resp, err := s.primary.Search(ctx, req)
	if err != nil {
		return resp, err //nolint:wrapcheck // pass the primary searcher's error through unchanged.
	}
	if !s.shouldFallback(resp, req) {
		return resp, nil
	}
	results, provErr := s.searchProvider(ctx, req.SubmittedText(), req.Limit)
	if provErr != nil {
		slog.DebugContext(ctx, msgFallbackFailed, slog.Any("error", provErr))
		resp.PartialFailures = append(resp.PartialFailures, webProviderFailure())

		return resp, nil
	}
	results = verifiedWebResults(req, results)
	resp.Results = toCoreResults(results, req.Limit)
	resp.TotalResults = len(resp.Results)
	if s.seeder != nil && len(results) > 0 {
		s.seedWebResults(ctx, results)
	}

	return resp, nil
}

func webProviderFailure() searchcore.PartialFailure {
	return searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceWeb,
		Reason: msgFallbackFailed,
	}
}

func resultURLs(results []Result) []string {
	urls := make([]string, 0, len(results))
	for _, result := range results {
		if result.URL != "" {
			urls = append(urls, result.URL)
		}
	}

	return urls
}

func (s *FallbackSearcher) shouldFallback(resp searchcore.Response, req searchcore.Request) bool {
	return len(resp.Results) == 0 && s.providerEligible(req)
}

func toCoreResults(results []Result, limit int) []searchcore.Result {
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	out := make([]searchcore.Result, 0, len(results))
	for rank, result := range results {
		out = append(out, searchcore.Result{
			Title:      result.Title,
			URL:        result.URL,
			DisplayURL: result.URL,
			Snippet:    result.Snippet,
			Score:      1 - float64(rank)*webResultDecay,
			Source:     searchcore.SourceWeb,
			Host:       resultHost(result.URL),
		})
	}

	return out
}

func resultHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return parsed.Host
}
