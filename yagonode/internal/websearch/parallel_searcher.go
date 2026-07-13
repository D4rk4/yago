package websearch

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type ParallelSearcher struct {
	fallback *FallbackSearcher
}

type parallelPrimaryOutcome struct {
	response searchcore.Response
	err      error
	failure  any
}

type parallelProviderOutcome struct {
	results []Result
	err     error
	failure any
}

func NewParallelSearcher(
	primary searchcore.Searcher,
	provider Provider,
	permit func(searchcore.Request) bool,
	opts ...Option,
) *ParallelSearcher {
	return &ParallelSearcher{fallback: NewFallbackSearcher(primary, provider, permit, opts...)}
}

func (s *ParallelSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if !s.fallback.providerEligible(req) {
		response, err := s.fallback.primary.Search(ctx, req)
		if err != nil {
			return response, fmt.Errorf("search primary: %w", err)
		}

		return response, nil
	}

	branchContext, cancel := context.WithCancel(ctx)
	defer cancel()

	primaryOutcome := make(chan parallelPrimaryOutcome, 1)
	providerOutcome := make(chan parallelProviderOutcome, 1)
	go func() {
		outcome := parallelPrimaryOutcome{}
		defer func() {
			outcome.failure = recover()
			primaryOutcome <- outcome
		}()
		outcome.response, outcome.err = s.fallback.primary.Search(branchContext, req)
	}()
	go func() {
		outcome := parallelProviderOutcome{}
		defer func() {
			outcome.failure = recover()
			providerOutcome <- outcome
		}()
		outcome.results, outcome.err = s.fallback.searchProvider(
			branchContext,
			req.SubmittedText(),
			req.Limit,
		)
	}()

	primary := <-primaryOutcome
	if primary.failure != nil {
		cancel()
		panic(primary.failure)
	}
	if primary.err != nil {
		cancel()

		return primary.response, fmt.Errorf("search primary: %w", primary.err)
	}
	provider := <-providerOutcome
	if provider.failure != nil {
		panic(provider.failure)
	}
	if provider.err != nil {
		return failedParallelProviderResponse(ctx, primary.response, provider.err), nil
	}

	provider.results = verifiedWebResults(req, provider.results)
	if s.fallback.seeder != nil && len(provider.results) > 0 {
		s.fallback.seedWebResults(ctx, provider.results)
	}
	webResults := toCoreResults(provider.results, req.Limit)
	if len(webResults) == 0 {
		return primary.response, nil
	}

	webResults = parallelResultIdentities(primary.response.Results, webResults)
	merged := searchcore.FuseByReciprocalRank(primary.response.Results, webResults)
	duplicateCount := len(primary.response.Results) + len(webResults) - len(merged)
	primary.response.TotalResults += len(webResults) - duplicateCount
	if req.Limit > 0 && len(merged) > req.Limit {
		merged = merged[:req.Limit]
	}
	primary.response.Results = merged
	primary.response.Request = req

	return primary.response, nil
}

func failedParallelProviderResponse(
	ctx context.Context,
	response searchcore.Response,
	err error,
) searchcore.Response {
	slog.DebugContext(ctx, msgFallbackFailed, slog.Any("error", err))
	response.PartialFailures = append(response.PartialFailures, webProviderFailure())

	return response
}

func parallelResultIdentities(
	primary []searchcore.Result,
	web []searchcore.Result,
) []searchcore.Result {
	hashes := make(map[string]string, len(primary))
	for _, result := range primary {
		if result.URLHash != "" {
			hashes[result.URL] = result.URLHash
		}
	}
	identified := slices.Clone(web)
	for index := range identified {
		identified[index].URLHash = hashes[identified[index].URL]
	}

	return identified
}

func (s *FallbackSearcher) providerEligible(req searchcore.Request) bool {
	if s.provider == nil ||
		(req.Source == searchcore.SourceLocal && !req.AllowWebFallback) {
		return false
	}
	if req.ContentDomain != "" && req.ContentDomain != searchcore.ContentDomainText {
		return false
	}
	if s.permit == nil || !s.permit(req) {
		return false
	}

	return strings.TrimSpace(req.SubmittedText()) != ""
}
