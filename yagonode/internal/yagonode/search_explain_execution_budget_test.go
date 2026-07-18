package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type blockedSearchExplanationSource struct {
	started chan struct{}
	release chan struct{}
	done    chan struct{}
}

func TestSearchExplanationExecutionBudgetHandlesShortBudgetAndPanic(t *testing.T) {
	short := searchExplanationExecutionBudget{
		budget:    10 * time.Millisecond,
		grace:     20 * time.Millisecond,
		admission: newInteractiveSearchAdmission(1),
		panicLog:  func(context.Context, string, ...any) {},
	}
	if outcome, err := short.execute(
		t.Context(),
		func(context.Context) (searchExplainOutcome, error) {
			return searchExplainOutcome{}, nil
		},
	); err != nil || len(outcome.partialFailures) != 0 {
		t.Fatalf("short execution = %#v, %v", outcome, err)
	}
	logged := false
	panicValue := func() (recovered any) {
		defer func() { recovered = recover() }()
		panicBudget := newSearchExplanationExecutionBudget()
		panicBudget.admission = newInteractiveSearchAdmission(1)
		panicBudget.panicLog = func(context.Context, string, ...any) { logged = true }
		_, _ = panicBudget.execute(
			t.Context(),
			func(context.Context) (searchExplainOutcome, error) { panic("explain panic") },
		)

		return nil
	}()
	if panicValue != "explain panic" || !logged {
		t.Fatalf("panic = %#v, logged = %t", panicValue, logged)
	}
}

func TestSearchExplanationExecutionFailures(t *testing.T) {
	busyAdmission := newInteractiveSearchAdmission(1)
	busyAdmission.slots <- struct{}{}
	busy := searchExplanationExecutionBudget{
		budget:    20 * time.Millisecond,
		grace:     5 * time.Millisecond,
		admission: busyAdmission,
		panicLog:  func(context.Context, string, ...any) {},
	}
	busyOutcome, err := busy.execute(
		t.Context(),
		func(context.Context) (searchExplainOutcome, error) {
			t.Fatal("busy execution ran")

			return searchExplainOutcome{}, nil
		},
	)
	<-busyAdmission.slots
	if err != nil || len(busyOutcome.partialFailures) != 1 ||
		busyOutcome.partialFailures[0].Reason != interactiveSearchTimeoutFailure {
		t.Fatalf("busy outcome = %#v, %v", busyOutcome, err)
	}
	deadlineOutcome, err := newSearchExplanationExecutionBudget().execute(
		t.Context(),
		func(context.Context) (searchExplainOutcome, error) {
			return searchExplainOutcome{}, context.DeadlineExceeded
		},
	)
	if err != nil || len(deadlineOutcome.partialFailures) != 1 ||
		deadlineOutcome.partialFailures[0].Reason != interactiveSearchTimeoutFailure {
		t.Fatalf("deadline outcome = %#v, %v", deadlineOutcome, err)
	}
	capacityOutcome, err := failedSearchExplanationExecution(
		t.Context(),
		errInteractiveSearchCapacity,
	)
	if err != nil || len(capacityOutcome.partialFailures) != 1 ||
		capacityOutcome.partialFailures[0].Reason != interactiveSearchCapacityFailure {
		t.Fatalf("capacity outcome = %#v, %v", capacityOutcome, err)
	}
	canceledContext, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = failedSearchExplanationExecution(canceledContext, errors.New("ignored"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
}

func (s blockedSearchExplanationSource) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	close(s.started)
	<-s.release
	close(s.done)

	return searchcore.Response{}, nil
}

func TestSearchExplanationBudgetBoundsLocalAndGlobalRanking(t *testing.T) {
	for _, scope := range []searchcore.Source{searchcore.SourceLocal, searchcore.SourceGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			blocked := blockedSearchExplanationSource{
				started: make(chan struct{}),
				release: make(chan struct{}),
				done:    make(chan struct{}),
			}
			index := searchindex.SearchIndex(nil)
			if scope == searchcore.SourceLocal {
				index = &searchExplainScript{}
			}
			endpoint := newSearchExplainEndpoint(index, nil, nil, nil, nil).withGlobal(blocked)
			endpoint.execution = searchExplanationExecutionBudget{
				budget:    40 * time.Millisecond,
				grace:     10 * time.Millisecond,
				admission: newInteractiveSearchAdmission(1),
				panicLog:  func(context.Context, string, ...any) {},
			}
			startedAt := time.Now()
			response, status, err := endpoint.explanation(t.Context(), searchExplainRequest{
				Query: "alpha", Scope: scope,
			})
			elapsed := time.Since(startedAt)
			<-blocked.started
			close(blocked.release)
			<-blocked.done
			if err != nil || status != 200 {
				t.Fatalf("explanation = %#v, %d, %v", response, status, err)
			}
			if elapsed >= 200*time.Millisecond {
				t.Fatalf("elapsed = %s", elapsed)
			}
			if len(response.PartialFailures) != 1 ||
				response.PartialFailures[0].Reason != interactiveSearchTimeoutFailure {
				t.Fatalf("partial failures = %#v", response.PartialFailures)
			}
		})
	}
}

func TestSearchExplanationBudgetIncludesPostRetrievalWork(t *testing.T) {
	execution := searchExplanationExecutionBudget{
		budget:    40 * time.Millisecond,
		grace:     10 * time.Millisecond,
		admission: newInteractiveSearchAdmission(1),
		panicLog:  func(context.Context, string, ...any) {},
	}
	postRetrieval := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	outcome, err := execution.execute(
		t.Context(),
		func(context.Context) (searchExplainOutcome, error) {
			close(postRetrieval)
			<-release
			close(done)

			return searchExplainOutcome{}, nil
		},
	)
	<-postRetrieval
	close(release)
	<-done
	if err != nil || len(outcome.partialFailures) != 1 ||
		outcome.partialFailures[0].Reason != interactiveSearchTimeoutFailure {
		t.Fatalf("outcome = %#v, error = %v", outcome, err)
	}
}
