package websearch

import (
	"context"
	"fmt"
	"slices"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type ResultRecovery func(context.Context, searchcore.Request, searchcore.Response) (searchcore.Response, error)

func WithRecovery(recovery ResultRecovery) Option {
	return func(searcher *FallbackSearcher) { searcher.recovery = recovery }
}

func (s *FallbackSearcher) recoverResults(
	ctx context.Context,
	request searchcore.Request,
	response searchcore.Response,
) (searchcore.Response, error) {
	if s.recovery == nil || len(response.Results) > 0 {
		return response, nil
	}
	response.Results = slices.Clone(response.Results)
	response.PartialFailures = slices.Clone(response.PartialFailures)
	recovered, err := s.recovery(ctx, request, response)
	if err != nil {
		return recovered, fmt.Errorf("recover primary search: %w", err)
	}
	return recovered, nil
}

func (s *FallbackSearcher) supplement(
	ctx context.Context,
	request searchcore.Request,
	primary searchcore.Response,
	known map[string]struct{},
) (searchcore.Response, error) {
	initial := primary
	branch, cancel := context.WithCancel(ctx)
	defer cancel()
	recovery := s.startRecovery(branch, request, initial)
	provider := make(chan parallelProviderOutcome, 1)
	go func() {
		outcome := parallelProviderOutcome{}
		defer func() { outcome.failure = recover(); provider <- outcome }()
		outcome = s.searchSupplement(branch, request, known)
	}()
	outcomes := collectSupplementOutcomes(ctx, recovery, provider)
	if outcomes.primary.failure != nil {
		panic(outcomes.primary.failure)
	}
	if outcomes.provider.failure != nil {
		panic(outcomes.provider.failure)
	}
	if outcomes.primaryReady {
		primary = outcomes.primary.response
		if outcomes.primary.err != nil {
			primary = failedParallelPrimaryResponse(primary)
		}
	}
	if !outcomes.providerReady {
		outcomes.provider.err = context.Cause(ctx)
	}
	return s.completeWebSearch(ctx, request, primary, outcomes.provider)
}

func collectSupplementOutcomes(
	ctx context.Context,
	recovery <-chan parallelPrimaryOutcome,
	provider <-chan parallelProviderOutcome,
) parallelOutcomes {
	return collectSearchOutcomes(ctx, parallelOutcomes{}, recovery, provider, true)
}

func collectSearchOutcomes(
	ctx context.Context,
	outcomes parallelOutcomes,
	recovery <-chan parallelPrimaryOutcome,
	provider <-chan parallelProviderOutcome,
	finishOnWeb bool,
) parallelOutcomes {
	for !outcomes.primaryReady || !outcomes.providerReady {
		select {
		case outcomes.primary = <-recovery:
			outcomes.primaryReady = true
			recovery = nil
		case outcomes.provider = <-provider:
			outcomes.providerReady = true
			provider = nil
			if finishOnWeb &&
				(len(outcomes.provider.results) > 0 || outcomes.provider.failure != nil) {
				select {
				case outcomes.primary = <-recovery:
					outcomes.primaryReady = true
				default:
				}
				return outcomes
			}
		case <-ctx.Done():
			return drainParallelOutcomes(outcomes, recovery, provider)
		}
	}
	return outcomes
}
