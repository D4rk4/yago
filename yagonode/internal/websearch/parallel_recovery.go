package websearch

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func (s *ParallelSearcher) collectSearch(
	ctx context.Context,
	request searchcore.Request,
	primary <-chan parallelPrimaryOutcome,
	provider <-chan parallelProviderOutcome,
) parallelOutcomes {
	if s.fallback.recovery == nil {
		return collectParallelOutcomes(ctx, primary, provider)
	}
	select {
	case initial := <-primary:
		if initial.err != nil || initial.failure != nil || len(initial.response.Results) > 0 {
			return collectSearchOutcomes(
				ctx,
				parallelOutcomes{primary: initial, primaryReady: true},
				nil,
				provider,
				false,
			)
		}
		recovered := s.fallback.startRecovery(ctx, request, initial.response)
		outcomes := collectSupplementOutcomes(ctx, recovered, provider)
		if !outcomes.primaryReady {
			outcomes.primary, outcomes.primaryReady = initial, true
		}
		return outcomes
	case <-ctx.Done():
		return drainParallelOutcomes(parallelOutcomes{}, primary, provider)
	}
}

func (s *FallbackSearcher) startRecovery(
	ctx context.Context,
	request searchcore.Request,
	primary searchcore.Response,
) <-chan parallelPrimaryOutcome {
	recovery := make(chan parallelPrimaryOutcome, 1)
	go func() {
		outcome := parallelPrimaryOutcome{}
		defer func() { outcome.failure = recover(); recovery <- outcome }()
		outcome.response, outcome.err = s.recoverResults(ctx, request, primary)
	}()
	return recovery
}
