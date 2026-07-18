package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const searchExplanationPanicMessage = "search explanation pipeline panicked"

type searchExplanationExecutionBudget struct {
	budget    time.Duration
	grace     time.Duration
	admission *interactiveSearchAdmission
	panicLog  func(context.Context, string, ...any)
}

type searchExplanationExecutionOutcome struct {
	explanation searchExplainOutcome
	err         error
	failure     any
}

func newSearchExplanationExecutionBudget() searchExplanationExecutionBudget {
	return searchExplanationExecutionBudget{
		budget:    interactiveSearchBudget,
		grace:     interactiveSearchCancellationGrace,
		admission: processInteractiveSearchAdmission,
		panicLog:  slog.ErrorContext,
	}
}

func (b searchExplanationExecutionBudget) execute(
	ctx context.Context,
	run func(context.Context) (searchExplainOutcome, error),
) (searchExplainOutcome, error) {
	hardContext, hardCancel := context.WithTimeout(ctx, b.budget)
	defer hardCancel()
	workBudget := b.budget - b.grace
	if workBudget <= 0 {
		workBudget = b.budget / 2
	}
	workContext, workCancel := context.WithTimeout(hardContext, workBudget)
	defer workCancel()
	release, err := b.admission.acquire(workContext)
	if err != nil {
		return failedSearchExplanationExecution(ctx, err)
	}
	outcomes := make(chan searchExplanationExecutionOutcome, 1)
	go b.run(workContext, release, run, outcomes)

	select {
	case outcome := <-outcomes:
		if outcome.failure != nil {
			panic(outcome.failure)
		}
		if outcome.err != nil {
			return failedSearchExplanationExecution(ctx, outcome.err)
		}

		return outcome.explanation, nil
	case <-hardContext.Done():
		return failedSearchExplanationExecution(ctx, context.Cause(hardContext))
	}
}

func (b searchExplanationExecutionBudget) run(
	ctx context.Context,
	release func(),
	run func(context.Context) (searchExplainOutcome, error),
	outcomes chan<- searchExplanationExecutionOutcome,
) {
	outcome := searchExplanationExecutionOutcome{}
	defer func() {
		outcome.failure = recover()
		if outcome.failure != nil {
			b.panicLog(ctx, searchExplanationPanicMessage, slog.Any("panic", outcome.failure))
		}
		release()
		outcomes <- outcome
	}()
	outcome.explanation, outcome.err = run(ctx)
}

func failedSearchExplanationExecution(
	ctx context.Context,
	err error,
) (searchExplainOutcome, error) {
	if cause := context.Cause(ctx); cause != nil {
		return searchExplainOutcome{}, fmt.Errorf("search explanation: %w", cause)
	}
	if !errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, errInteractiveSearchCapacity) {
		return searchExplainOutcome{}, err
	}
	response, _ := interactiveSearchResult(
		searchcore.Request{},
		searchcore.Response{},
		err,
	)

	return searchExplainOutcome{partialFailures: response.PartialFailures}, nil
}
