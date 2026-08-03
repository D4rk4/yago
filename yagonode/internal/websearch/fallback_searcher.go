package websearch

import (
	"context"
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
	primary        searchcore.Searcher
	provider       Provider
	permit         func(searchcore.Request) bool
	seeder         CrawlSeeder
	providerBudget time.Duration
	spawnSeedWork  func(string, context.Context, func(context.Context)) bool
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
	resp, err := s.primary.Search(ctx, req)
	if err != nil {
		return resp, err //nolint:wrapcheck // pass the primary searcher's error through unchanged.
	}
	if !s.shouldFallback(resp, req) {
		return resp, nil
	}
	results, provErr := s.searchProvider(ctx, req, req.Limit)
	// See ParallelSearcher.Search: seeding takes the engines' accepted rows,
	// before the caller's own constraints narrow what is served.
	discovered := relevantWebResults(req, results)
	results = resultsMatchingConstraints(req, discovered)
	if provErr != nil {
		logProviderFailure(ctx, provErr)
		resp.PartialFailures = append(resp.PartialFailures, webProviderFailure())
	}
	webResults := toCoreResults(results, req.Limit)
	if provErr != nil && len(webResults) == 0 {
		return resp, nil
	}
	clearPrimaryMissRecoveryForWebAnswer(&resp, webResults)
	resp.Results = webResults
	resp.TotalResults = len(resp.Results)
	if s.seeder != nil && len(discovered) > 0 {
		s.seedWebResults(ctx, discovered)
	}

	return resp, nil
}

// logProviderFailure records a lost provider stage.
//
// Both call sites logged this at Debug. Production runs at Info, so the line
// never appeared and an operator watching half of all searches lose the
// provider saw only the aggregate outage warning. Info matches the treatment of
// the other silent loss in this tree, the ingest quality gate: the caller is
// already told through a partial failure, so this is the record that says a
// stage was lost.
//
// The error itself is deliberately not attached. Its text carries the request
// URL, and the request URL carries the submitted query, so logging it would
// publish what the caller searched for -- which
// TestUnavailableLoggingDoesNotExposeSubmittedQuery exists to prevent, and
// which it caught when this function first did exactly that. The per-engine
// record in ddgs_engine_race.go carries the actionable detail instead: it names
// the engine, whether it was rate limited, and how many results it fetched
// against how many survived acceptance, and it is query-free by construction.
func logProviderFailure(ctx context.Context, err error) {
	slog.InfoContext(
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

	return parsed.Hostname()
}
