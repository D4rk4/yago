package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

var (
	recoverySearchBudget           = 150 * time.Millisecond
	processRecoverySearchAdmission = newInteractiveSearchAdmission(
		interactiveSearchConcurrentWork,
	)
)

const (
	recoverySearchCancellationGrace = 25 * time.Millisecond
	recoverySearchFailureSource     = "fuzzy-stage"
	recoverySearchTimeoutFailure    = "fuzzy search deadline exceeded"
	recoverySearchCapacityFailure   = "fuzzy search capacity exhausted"
	recoverySearchFailed            = "fuzzy search failed"
	recoverySearchPanicMessage      = "fuzzy search stage panicked"
)

type recoveryBudgetSearcher struct {
	inner     searchcore.Searcher
	budget    time.Duration
	grace     time.Duration
	admission *interactiveSearchAdmission
	panicLog  func(context.Context, string, ...any)
}

type recoverySearchOutcome struct {
	response searchcore.Response
	err      error
	failure  any
}

func withRecoverySearchBudget(inner searchcore.Searcher) searchcore.Searcher {
	return recoveryBudgetSearcher{
		inner:     inner,
		budget:    recoverySearchBudget,
		grace:     recoverySearchCancellationGrace,
		admission: processRecoverySearchAdmission,
		panicLog:  slog.ErrorContext,
	}
}

func (s recoveryBudgetSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	hardContext, hardCancel := context.WithTimeout(ctx, s.budget)
	defer hardCancel()
	stageBudget := s.budget - s.grace
	if stageBudget <= 0 {
		stageBudget = s.budget / 2
	}
	stageContext, stageCancel := context.WithTimeout(hardContext, stageBudget)
	defer stageCancel()

	release, err := s.admission.tryAcquire(stageContext)
	if err != nil {
		if errors.Is(err, errInteractiveSearchCapacity) {
			return recoverySearchFailure(req, recoverySearchCapacityFailure), nil
		}

		return searchcore.Response{}, fmt.Errorf("fuzzy search admission: %w", err)
	}
	outcomes := make(chan recoverySearchOutcome, 1)
	go s.run(stageContext, req, release, outcomes)

	select {
	case outcome := <-outcomes:
		if outcome.failure != nil {
			panic(outcome.failure)
		}
		if outcome.err == nil {
			return outcome.response, nil
		}
		if errors.Is(outcome.err, context.DeadlineExceeded) {
			outcome.response.Request = req
			outcome.response.PartialFailures = append(
				outcome.response.PartialFailures,
				searchcore.PartialFailure{
					Source: recoverySearchFailureSource,
					Reason: recoverySearchTimeoutFailure,
				},
			)

			return outcome.response, nil
		}
		outcome.response.Request = req
		slog.WarnContext(ctx, recoverySearchFailed, slog.Any("error", outcome.err))
		outcome.response.PartialFailures = append(
			outcome.response.PartialFailures,
			searchcore.PartialFailure{
				Source: recoverySearchFailureSource,
				Reason: recoverySearchFailed,
			},
		)

		return outcome.response, nil
	case <-hardContext.Done():
		if ctx.Err() != nil {
			return searchcore.Response{}, fmt.Errorf("fuzzy search: %w", ctx.Err())
		}

		return recoverySearchFailure(req, recoverySearchTimeoutFailure), nil
	}
}

func (s recoveryBudgetSearcher) run(
	ctx context.Context,
	req searchcore.Request,
	release func(),
	outcomes chan<- recoverySearchOutcome,
) {
	outcome := recoverySearchOutcome{}
	defer func() {
		outcome.failure = recover()
		if outcome.failure != nil {
			s.panicLog(ctx, recoverySearchPanicMessage, slog.Any("panic", outcome.failure))
		}
		release()
		outcomes <- outcome
	}()
	outcome.response, outcome.err = s.inner.Search(ctx, req)
}

func recoverySearchFailure(
	req searchcore.Request,
	reason string,
) searchcore.Response {
	return searchcore.Response{
		Request: req,
		PartialFailures: []searchcore.PartialFailure{{
			Source: recoverySearchFailureSource,
			Reason: reason,
		}},
	}
}
