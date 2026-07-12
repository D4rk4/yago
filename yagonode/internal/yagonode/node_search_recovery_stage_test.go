package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestRecoveryStageRetainsAdmissionUntilUncooperativeWorkStops(t *testing.T) {
	inner := &blockingInteractiveSearch{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	admission := newInteractiveSearchAdmission(1)
	searcher := recoveryBudgetSearcher{
		inner: inner, budget: 20 * time.Millisecond, grace: 10 * time.Millisecond,
		admission: admission, panicLog: discardInteractiveSearchPanic,
	}
	response, err := searcher.Search(
		t.Context(),
		searchcore.Request{Query: "slow", Fuzzy: true},
	)
	if err != nil || len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Reason != recoverySearchTimeoutFailure {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	if len(admission.slots) != 1 {
		t.Fatalf("active fuzzy stages = %d", len(admission.slots))
	}
	busy, err := searcher.Search(t.Context(), searchcore.Request{Query: "busy", Fuzzy: true})
	if err != nil || len(busy.PartialFailures) != 1 ||
		busy.PartialFailures[0].Reason != recoverySearchCapacityFailure ||
		inner.calls.Load() != 1 {
		t.Fatalf("busy = %#v, error = %v, calls = %d", busy, err, inner.calls.Load())
	}
	close(inner.release)
	select {
	case <-inner.finished:
	case <-time.After(time.Second):
		t.Fatal("fuzzy work did not finish")
	}
}

func TestRecoveryStagePreservesCanceledAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	searcher := recoveryBudgetSearcher{
		inner: staticSearcher{}, budget: time.Second, grace: time.Millisecond,
		admission: newInteractiveSearchAdmission(1), panicLog: discardInteractiveSearchPanic,
	}
	_, err := searcher.Search(ctx, searchcore.Request{Query: "canceled", Fuzzy: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestRecoveryStageForwardsAndReportsPanic(t *testing.T) {
	reported := make(chan any, 1)
	searcher := recoveryBudgetSearcher{
		inner:  panicSearcher{failure: "fuzzy panic"},
		budget: time.Second, grace: time.Millisecond,
		admission: newInteractiveSearchAdmission(1),
		panicLog: func(_ context.Context, _ string, attributes ...any) {
			reported <- attributes[0]
		},
	}
	defer func() {
		if recover() != "fuzzy panic" {
			t.Fatal("fuzzy panic was not forwarded")
		}
		select {
		case <-reported:
		default:
			t.Fatal("fuzzy panic was not reported")
		}
	}()
	_, _ = searcher.Search(t.Context(), searchcore.Request{Query: "panic", Fuzzy: true})
}

func TestRecoveryStagePreservesParentCancellation(t *testing.T) {
	inner := &blockingInteractiveSearch{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	searcher := recoveryBudgetSearcher{
		inner: inner, budget: time.Hour, grace: time.Minute,
		admission: newInteractiveSearchAdmission(1), panicLog: discardInteractiveSearchPanic,
	}
	ctx, cancel := context.WithCancel(t.Context())
	errorsReturned := make(chan error, 1)
	go func() {
		_, err := searcher.Search(ctx, searchcore.Request{Query: "cancel", Fuzzy: true})
		errorsReturned <- err
	}()
	<-inner.started
	cancel()
	if err := <-errorsReturned; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	close(inner.release)
	select {
	case <-inner.finished:
	case <-time.After(time.Second):
		t.Fatal("fuzzy work did not finish")
	}
}
