package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestWebFallbackExactStageReturnsBeforeUncooperativeWorkStops(t *testing.T) {
	inner := &blockingInteractiveSearch{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	admission := newInteractiveSearchAdmission(1)
	searcher := webFallbackExactStageBudgetSearcher{
		inner: inner, permit: func(searchcore.Request) bool { return true },
		budget: 20 * time.Millisecond, grace: 10 * time.Millisecond,
		admission: admission, panicLog: discardInteractiveSearchPanic,
	}

	started := time.Now()
	response, err := searcher.Search(
		t.Context(),
		searchcore.Request{Query: "slow", Terms: []string{"slow"}},
	)
	if err != nil || time.Since(started) > 200*time.Millisecond ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Reason != webFallbackExactStageTimeoutFailure {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	if len(admission.slots) != 1 {
		t.Fatalf("active exact stages = %d", len(admission.slots))
	}
	busy, err := searcher.Search(t.Context(), searchcore.Request{Query: "busy"})
	if err != nil || len(busy.PartialFailures) != 1 ||
		busy.PartialFailures[0].Reason != webFallbackExactStageCapacityFailure ||
		inner.calls.Load() != 1 {
		t.Fatalf("busy = %#v, error = %v, calls = %d", busy, err, inner.calls.Load())
	}

	close(inner.release)
	select {
	case <-inner.finished:
	case <-time.After(time.Second):
		t.Fatal("exact work did not finish")
	}
	deadline := time.Now().Add(time.Second)
	for len(admission.slots) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("exact admission was not released")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestWebFallbackExactStageWrapsUnbudgetedError(t *testing.T) {
	want := errors.New("exact failed")
	searcher := webFallbackExactStageBudgetSearcher{
		inner:  errorInteractiveSearch{err: want},
		permit: func(searchcore.Request) bool { return false },
	}
	_, err := searcher.Search(t.Context(), searchcore.Request{Query: "failed"})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}

func TestWebFallbackExactStagePreservesCanceledAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	searcher := webFallbackExactStageBudgetSearcher{
		inner: staticSearcher{}, permit: func(searchcore.Request) bool { return true },
		budget: time.Second, grace: time.Millisecond,
		admission: newInteractiveSearchAdmission(1), panicLog: discardInteractiveSearchPanic,
	}
	_, err := searcher.Search(ctx, searchcore.Request{Query: "canceled"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestWebFallbackExactStageForwardsAndReportsPanic(t *testing.T) {
	reported := make(chan any, 1)
	searcher := webFallbackExactStageBudgetSearcher{
		inner:  panicSearcher{failure: "exact panic"},
		permit: func(searchcore.Request) bool { return true },
		budget: time.Second, grace: time.Millisecond,
		admission: newInteractiveSearchAdmission(1),
		panicLog: func(_ context.Context, _ string, attributes ...any) {
			reported <- attributes[0]
		},
	}
	defer func() {
		if recover() != "exact panic" {
			t.Fatal("exact panic was not forwarded")
		}
		select {
		case <-reported:
		default:
			t.Fatal("exact panic was not reported")
		}
	}()
	_, _ = searcher.Search(t.Context(), searchcore.Request{Query: "panic"})
}

func TestWebFallbackExactStagePreservesParentCancellation(t *testing.T) {
	inner := &blockingInteractiveSearch{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	searcher := webFallbackExactStageBudgetSearcher{
		inner: inner, permit: func(searchcore.Request) bool { return true },
		budget: time.Hour, grace: time.Minute,
		admission: newInteractiveSearchAdmission(1), panicLog: discardInteractiveSearchPanic,
	}
	ctx, cancel := context.WithCancel(t.Context())
	errorsReturned := make(chan error, 1)
	go func() {
		_, err := searcher.Search(ctx, searchcore.Request{Query: "cancel"})
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
		t.Fatal("exact work did not finish")
	}
}

func TestRecoverySkipsFuzzyRetryAfterExactStageHardFailure(t *testing.T) {
	retry := &scriptedRecoverySearcher{fuzzyResults: []searchcore.Result{{
		Title: "Fuzzy slow", URL: "https://local.example/slow",
	}}}
	inner := staticSearcher{resp: webFallbackExactStageFailure(
		searchcore.Request{Query: "slow"},
		webFallbackExactStageTimeoutFailure,
	)}
	response, err := withZeroResultRecovery(inner, retry, nil).Search(
		t.Context(),
		searchcore.Request{Query: "slow", Terms: []string{"slow"}},
	)
	if err != nil || retry.fuzzyCalls != 0 || len(response.Results) != 0 {
		t.Fatalf("response = %#v, error = %v, fuzzy calls = %d", response, err, retry.fuzzyCalls)
	}
}
