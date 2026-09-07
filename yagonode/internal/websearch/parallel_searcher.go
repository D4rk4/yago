package websearch

import (
	"context"
	"errors"
	"fmt"
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
	results    []Result
	discovered []Result
	err        error
	failure    any
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
	outcomes := s.collectSearch(
		branchContext,
		req,
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
	response, err := s.fallback.completeWebSearch(ctx, req, primary.response, provider)
	if err == nil && len(response.Results) == 0 && primary.err != nil {
		return response, errParallelSearchUnavailable
	}
	return response, err
}

func (s *ParallelSearcher) startParallelSearches(
	ctx context.Context,
	req searchcore.Request,
) (<-chan parallelPrimaryOutcome, <-chan parallelProviderOutcome) {
	snapshot := &primaryResultSnapshot{ready: make(chan struct{})}
	primaryOutcome := make(chan parallelPrimaryOutcome, 1)
	providerOutcome := make(chan parallelProviderOutcome, 1)
	go func() {
		outcome := parallelPrimaryOutcome{}
		defer func() {
			outcome.failure = recover()
			_, snapshot.known = primaryCandidates(req, outcome.response)
			close(snapshot.ready)
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
		prepared := newProviderQueryForRequest(req)
		prepared.primary = snapshot
		outcome.results, outcome.err = s.fallback.searchPreparedProvider(
			ctx,
			prepared,
			maxCachedResults,
		)
		outcome.discovered = relevantWebResults(req, outcome.results)
		outcome.results = novelContribution(
			verifiedWebResults(req, outcome.results),
			prepared.known(ctx),
		)
	}()

	return primaryOutcome, providerOutcome
}

func collectParallelOutcomes(
	ctx context.Context,
	primaryOutcomes <-chan parallelPrimaryOutcome,
	providerOutcomes <-chan parallelProviderOutcome,
) parallelOutcomes {
	return collectSearchOutcomes(ctx, parallelOutcomes{}, primaryOutcomes, providerOutcomes, false)
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
		response.Recovered = ""
		response.DidYouMean = ""
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
	logProviderFailure(ctx, err)
	response.PartialFailures = append(response.PartialFailures, webProviderFailure())

	return response
}

func parallelResultIdentities(
	primary []searchcore.Result,
	web []searchcore.Result,
) []searchcore.Result {
	positions := make(map[string]int, len(primary))
	for index, result := range primary {
		positions[candidateURL(result.URL)] = index
	}
	identified := slices.Clone(web)
	for index := range identified {
		if position, exists := positions[candidateURL(identified[index].URL)]; exists {
			identified[index].URLHash = primary[position].URLHash
			identified[index].URL = primary[position].URL
		}
	}

	return identified
}

func (s *FallbackSearcher) providerEligible(req searchcore.Request) bool {
	if s.provider == nil ||
		req.Source == searchcore.SourceLocal {
		return false
	}
	if req.ContentDomain != "" && req.ContentDomain != searchcore.ContentDomainText {
		return false
	}
	// A first-seen-bounded request asks when this node first saw a page, which
	// only this node's own index knows. Provider rows are not held here, so every
	// one of them is dropped again by the caller's window: running the provider
	// would send the query to an external engine, spend the shared provider
	// budget, and add its latency to buy rows that cannot qualify. Same rule as
	// the vertical above — a request shape the provider cannot answer does not
	// buy a call. The cost is that such a request also seeds no crawl URLs; a
	// request without the bound still does, and seeding is what makes pages
	// locally held and so first-seen at all.
	if req.FirstSeenBounded() {
		return false
	}
	if s.permit == nil || !s.permit(req) {
		return false
	}

	return strings.TrimSpace(req.SubmittedText()) != ""
}
