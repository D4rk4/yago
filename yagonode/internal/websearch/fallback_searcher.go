package websearch

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	msgFallbackFailed = "web-search fallback provider failed"
	webResultDecay    = 0.01
)

// FallbackSearcher wraps a primary searcher and, only on a true miss (the primary
// returned zero results) and only when the privacy policy permits this request,
// augments the response with results from a web-search Provider stamped as
// SourceWeb. The permit closure receives the request so a per-request opt-in
// policy can gate whether the query is allowed to leave the node.
type FallbackSearcher struct {
	primary  searchcore.Searcher
	provider Provider
	permit   func(searchcore.Request) bool
	seeder   CrawlSeeder
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
	results, provErr := s.provider.Search(ctx, req.Query, req.Limit)
	if provErr != nil {
		slog.DebugContext(ctx, msgFallbackFailed, slog.Any("error", provErr))

		return resp, nil
	}
	results = verifiedWebResults(req, results)
	resp.Results = toCoreResults(results, req.Limit)
	resp.TotalResults = len(resp.Results)
	if s.seeder != nil && len(results) > 0 {
		s.seeder.Seed(ctx, resultURLs(results))
	}

	return resp, nil
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
	if len(resp.Results) > 0 || s.provider == nil {
		return false
	}
	if s.permit == nil || !s.permit(req) {
		return false
	}

	return strings.TrimSpace(req.Query) != ""
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
