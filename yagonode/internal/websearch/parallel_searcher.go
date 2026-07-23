package websearch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type ParallelSearcher struct {
	fallback *FallbackSearcher
}

var errParallelSearchUnavailable = errors.New("parallel search unavailable")

const msgParallelPrimaryFailed = "primary search failed"

const parallelOutcomeCancellationGrace = 25 * time.Millisecond

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

type parallelOutcomes struct {
	primary       parallelPrimaryOutcome
	provider      parallelProviderOutcome
	primaryReady  bool
	providerReady bool
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

	primaryOutcome, providerOutcome := s.startParallelSearches(branchContext, req)
	outcomes := collectParallelOutcomes(
		ctx,
		primaryOutcome,
		providerOutcome,
	)
	primary := outcomes.primary
	provider := outcomes.provider
	if primary.failure != nil {
		cancel()
		panic(primary.failure)
	}
	if provider.failure != nil {
		cancel()
		panic(provider.failure)
	}
	if !outcomes.primaryReady {
		primary.err = context.Cause(ctx)
	}
	if !outcomes.providerReady {
		provider.err = context.Cause(ctx)
	}
	primary.response.Request = req
	if primary.err != nil {
		primary.response = failedParallelPrimaryResponse(primary.response)
	}
	provider.results = verifiedWebResults(req, provider.results)
	if provider.err != nil {
		primary.response = failedParallelProviderResponse(ctx, primary.response, provider.err)
	}
	if s.fallback.seeder != nil && len(provider.results) > 0 {
		s.fallback.seedWebResults(ctx, provider.results)
	}
	webResults := toCoreResults(provider.results, req.Limit)
	if len(primary.response.Results) > 0 || len(webResults) > 0 {
		return mergeParallelResults(primary.response, webResults, req), nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return primary.response, fmt.Errorf("parallel search: %w", cause)
	}
	if primary.err != nil {
		return primary.response, errParallelSearchUnavailable
	}

	return primary.response, nil
}

func (s *ParallelSearcher) startParallelSearches(
	ctx context.Context,
	req searchcore.Request,
) (<-chan parallelPrimaryOutcome, <-chan parallelProviderOutcome) {
	primaryOutcome := make(chan parallelPrimaryOutcome, 1)
	providerOutcome := make(chan parallelProviderOutcome, 1)
	go func() {
		outcome := parallelPrimaryOutcome{}
		defer func() {
			outcome.failure = recover()
			primaryOutcome <- outcome
		}()
		outcome.response, outcome.err = s.fallback.primary.Search(ctx, req)
	}()
	go func() {
		outcome := parallelProviderOutcome{}
		defer func() {
			outcome.failure = recover()
			providerOutcome <- outcome
		}()
		outcome.results, outcome.err = s.fallback.searchProvider(
			ctx,
			req,
			req.Limit,
		)
	}()

	return primaryOutcome, providerOutcome
}

func collectParallelOutcomes(
	ctx context.Context,
	primaryOutcomes <-chan parallelPrimaryOutcome,
	providerOutcomes <-chan parallelProviderOutcome,
) parallelOutcomes {
	outcomes := parallelOutcomes{}
	for !outcomes.primaryReady || !outcomes.providerReady {
		select {
		case outcomes.primary = <-primaryOutcomes:
			outcomes.primaryReady = true
		case outcomes.provider = <-providerOutcomes:
			outcomes.providerReady = true
		case <-ctx.Done():
			return drainParallelOutcomes(
				outcomes,
				primaryOutcomes,
				providerOutcomes,
			)
		}
	}

	return outcomes
}

func drainParallelOutcomes(
	outcomes parallelOutcomes,
	primaryOutcomes <-chan parallelPrimaryOutcome,
	providerOutcomes <-chan parallelProviderOutcome,
) parallelOutcomes {
	timer := time.NewTimer(parallelOutcomeCancellationGrace)
	defer timer.Stop()
	for !outcomes.primaryReady || !outcomes.providerReady {
		select {
		case outcomes.primary = <-primaryOutcomes:
			outcomes.primaryReady = true
		case outcomes.provider = <-providerOutcomes:
			outcomes.providerReady = true
		case <-timer.C:
			return outcomes
		}
	}

	return outcomes
}

func failedParallelPrimaryResponse(
	response searchcore.Response,
) searchcore.Response {
	response.PartialFailures = append(response.PartialFailures, searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceLocalSearch,
		Reason: msgParallelPrimaryFailed,
	})

	return response
}

func mergeParallelResults(
	response searchcore.Response,
	webResults []searchcore.Result,
	req searchcore.Request,
) searchcore.Response {
	if len(webResults) == 0 {
		return response
	}
	if len(response.Results) == 0 {
		clearPrimaryMissRecoveryForWebAnswer(&response, webResults)
		response.TotalResults = 0
	}
	webResults = parallelResultIdentities(response.Results, webResults)
	merged := searchcore.FuseByReciprocalRank(response.Results, webResults)
	duplicateCount := len(response.Results) + len(webResults) - len(merged)
	response.TotalResults = max(response.TotalResults, len(response.Results)) +
		len(webResults) - duplicateCount
	if req.Limit > 0 && len(merged) > req.Limit {
		merged = merged[:req.Limit]
	}
	response.Results = merged
	response.Request = req

	return response
}

func failedParallelProviderResponse(
	ctx context.Context,
	response searchcore.Response,
	err error,
) searchcore.Response {
	slog.DebugContext(
		ctx,
		msgFallbackFailed,
		slog.String("reason", webSearchFailureReason(err)),
	)
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
