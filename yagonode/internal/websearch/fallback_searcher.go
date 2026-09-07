package websearch

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	msgFallbackFailed    = "web-search fallback provider failed"
	msgWebSeedRejected   = "web-search crawl seeding saturated"
	msgWebSeedConsidered = "web-search crawl seeding considered"
	msgWebSeedPanicked   = "web-search crawl seeding panicked"
	webResultDecay       = 0.01
)

type FallbackSearcher struct {
	primary         searchcore.Searcher
	recovery        ResultRecovery
	provider        Provider
	permit          func(searchcore.Request) bool
	seeder          CrawlSeeder
	providerBudget  time.Duration
	providerReserve time.Duration
	spawnSeedWork   func(string, context.Context, func(context.Context)) bool
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
		searcher.spawnSeedWork = webSeedProcessAdmission().try
	}

	return searcher
}

func (s *FallbackSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	eligible := s.providerEligible(req)
	window := req
	if eligible {
		window = supplementalWindow(req)
	}
	response, err := s.primary.Search(ctx, window)
	if err != nil {
		return response, fmt.Errorf("primary search: %w", err)
	}
	if !eligible {
		return s.recoverResults(ctx, req, response)
	}
	response, known := primaryCandidates(window, response)
	if s.shouldFallback(response, window) {
		response, err = s.supplement(ctx, window, response, known)
	}
	return supplementalPage(response, req), err
}

func logProviderFailure(ctx context.Context, err error) {
	slog.WarnContext(
		ctx,
		msgFallbackFailed,
		slog.String("reason", webSearchFailureReason(err)),
	)
}

func webProviderFailure() searchcore.PartialFailure {
	return searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceWeb,
		Reason: msgFallbackFailed,
	}
}

func (s *FallbackSearcher) shouldFallback(resp searchcore.Response, req searchcore.Request) bool {
	return len(resp.Results) < SupplementalCandidateTarget && s.providerEligible(req)
}

func toCoreResults(results []Result, limit int) []searchcore.Result {
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	out := make([]searchcore.Result, 0, len(results))
	for rank, result := range results {
		out = append(out, searchcore.Result{
			Title:        result.Title,
			URL:          result.URL,
			DisplayURL:   result.URL,
			Snippet:      result.Snippet,
			Score:        1 - float64(rank)*webResultDecay,
			Source:       searchcore.SourceWeb,
			Host:         resultHost(result.URL),
			SafetyRating: webResultSafetyRating(result),
		})
	}

	return out
}

func resultHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	return parsed.Hostname()
}
