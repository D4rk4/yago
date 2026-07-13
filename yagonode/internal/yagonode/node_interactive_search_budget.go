package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const (
	interactiveSearchBudget            = 1800 * time.Millisecond
	interactiveSearchCancellationGrace = 50 * time.Millisecond
	interactiveSearchConcurrentWork    = 4
)

var processInteractiveSearchAdmission = newInteractiveSearchAdmission(
	interactiveSearchConcurrentWork,
)

type interactiveBudgetSearcher struct {
	inner     searchcore.Searcher
	budget    time.Duration
	grace     time.Duration
	admission *interactiveSearchAdmission
	panicLog  func(context.Context, string, ...any)
}

type interactiveSearchOutcome struct {
	response searchcore.Response
	err      error
	failure  any
}

const (
	interactiveSearchFailureSource  = searchcore.PartialFailureSourceLocalSearch
	interactiveSearchTimeoutFailure = "local search deadline exceeded"
	interactiveSearchPanicMessage   = "interactive search pipeline panicked"
)

func withInteractiveSearchBudget(inner searchcore.Searcher) searchcore.Searcher {
	return interactiveBudgetSearcher{
		inner:     inner,
		budget:    interactiveSearchBudget,
		grace:     interactiveSearchCancellationGrace,
		admission: processInteractiveSearchAdmission,
		panicLog:  slog.ErrorContext,
	}
}

func (s interactiveBudgetSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	hardCtx, hardCancel := context.WithTimeout(ctx, s.budget)
	defer hardCancel()
	searchCtx, searchCancel := context.WithTimeout(hardCtx, s.budget-s.grace)
	defer searchCancel()

	release, err := s.admission.tryAcquire(searchCtx)
	if err != nil {
		return interactiveSearchResult(req, searchcore.Response{}, err)
	}
	outcomes := make(chan interactiveSearchOutcome, 1)
	go s.run(searchCtx, req, release, outcomes)

	select {
	case outcome := <-outcomes:
		if outcome.failure != nil {
			panic(outcome.failure)
		}
		if outcome.err != nil {
			return interactiveSearchResult(req, searchcore.Response{}, outcome.err)
		}

		return outcome.response, nil
	case <-hardCtx.Done():
		return interactiveSearchResult(
			req,
			searchcore.Response{},
			context.Cause(hardCtx),
		)
	}
}

func (s interactiveBudgetSearcher) run(
	ctx context.Context,
	req searchcore.Request,
	release func(),
	outcomes chan<- interactiveSearchOutcome,
) {
	outcome := interactiveSearchOutcome{}
	defer func() {
		outcome.failure = recover()
		if outcome.failure != nil {
			s.panicLog(ctx, interactiveSearchPanicMessage, slog.Any("panic", outcome.failure))
		}
		release()
		outcomes <- outcome
	}()
	outcome.response, outcome.err = s.inner.Search(ctx, req)
}

func interactiveSearchError(err error) error {
	return fmt.Errorf("interactive search: %w", err)
}

func interactiveSearchResult(
	req searchcore.Request,
	response searchcore.Response,
	err error,
) (searchcore.Response, error) {
	if errors.Is(err, context.DeadlineExceeded) {
		return searchcore.Response{
			Request: req,
			PartialFailures: []searchcore.PartialFailure{{
				Source: interactiveSearchFailureSource,
				Reason: interactiveSearchTimeoutFailure,
			}},
		}, interactiveSearchError(err)
	}
	if errors.Is(err, errInteractiveSearchCapacity) {
		return searchcore.Response{
			Request: req,
			PartialFailures: []searchcore.PartialFailure{{
				Source: interactiveSearchFailureSource,
				Reason: interactiveSearchCapacityFailure,
			}},
		}, interactiveSearchError(err)
	}

	return response, interactiveSearchError(err)
}
