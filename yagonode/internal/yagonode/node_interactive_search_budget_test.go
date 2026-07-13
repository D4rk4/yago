package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchsession"
)

type deadlineSearch struct {
	cause chan error
}

type blockingInteractiveSearch struct {
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
	calls    atomic.Int32
}

type delayedPanicInteractiveSearch struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
	failure any
}

type countingInteractiveSearch struct {
	calls atomic.Int32
}

type interactiveSearchPanicRecord struct {
	message string
	value   any
}

func (s *blockingInteractiveSearch) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if s.calls.Add(1) == 1 {
		close(s.started)
		<-s.release
		close(s.finished)
	}

	return searchcore.Response{Request: req, TotalResults: 1}, nil
}

func (s *delayedPanicInteractiveSearch) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	if s.calls.Add(1) == 1 {
		close(s.started)
		<-s.release
		panic(s.failure)
	}

	return searchcore.Response{Request: req, TotalResults: 1}, nil
}

func (s *countingInteractiveSearch) Search(
	_ context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	s.calls.Add(1)

	return searchcore.Response{Request: req}, nil
}

func discardInteractiveSearchPanic(context.Context, string, ...any) {}

func interactiveBudgetFixture(
	inner searchcore.Searcher,
	budget time.Duration,
) searchcore.Searcher {
	return interactiveBudgetSearcher{
		inner:     inner,
		budget:    budget,
		grace:     budget / 2,
		admission: newInteractiveSearchAdmission(1),
		panicLog:  discardInteractiveSearchPanic,
	}
}

func (s *deadlineSearch) Search(
	ctx context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	<-ctx.Done()
	if s.cause != nil {
		s.cause <- context.Cause(ctx)
	}

	return searchcore.Response{}, fmt.Errorf("deadline search: %w", ctx.Err())
}

func TestInteractiveSearchBudgetCancelsSlowPipeline(t *testing.T) {
	inner := &deadlineSearch{cause: make(chan error, 1)}
	response, err := interactiveBudgetFixture(inner, 10*time.Millisecond).Search(
		t.Context(),
		searchcore.Request{Query: "slow"},
	)
	cause := <-inner.cause
	if !errors.Is(err, context.DeadlineExceeded) || response.Request.Query != "slow" ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Source != interactiveSearchFailureSource ||
		!errors.Is(cause, context.DeadlineExceeded) {
		t.Fatalf("deadline = response %#v error %v cause %v", response, err, cause)
	}
}

func TestInteractiveSearchBudgetKeepsCompletedLocalResults(t *testing.T) {
	local := staticSearcher{resp: searchcore.Response{Results: []searchcore.Result{{
		Title: "local", URL: "https://local.example/",
	}}}}
	response, err := interactiveBudgetFixture(
		searchcore.NewFederatedSearcher(local, &deadlineSearch{}),
		100*time.Millisecond,
	).Search(t.Context(), searchcore.Request{Source: searchcore.SourceGlobal, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].Title != "local" ||
		len(response.PartialFailures) != 1 {
		t.Fatalf("tail-tolerant response = %#v", response)
	}
}

func TestInteractiveSearchBudgetReturnsBeforeUncooperativeWorkStops(t *testing.T) {
	inner := &blockingInteractiveSearch{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		finished: make(chan struct{}),
	}
	admission := newInteractiveSearchAdmission(1)
	searcher := interactiveBudgetSearcher{
		inner: inner, budget: time.Hour, grace: time.Minute, admission: admission,
		panicLog: discardInteractiveSearchPanic,
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	result := make(chan interactiveSearchOutcome, 1)
	go func() {
		response, err := searcher.Search(ctx, searchcore.Request{Query: "slow"})
		result <- interactiveSearchOutcome{response: response, err: err}
	}()
	<-inner.started
	cancel(context.DeadlineExceeded)
	outcome := <-result
	if !errors.Is(outcome.err, context.DeadlineExceeded) ||
		outcome.response.Request.Query != "slow" ||
		len(outcome.response.PartialFailures) != 1 {
		t.Fatalf("hard deadline result = %#v, %v", outcome.response, outcome.err)
	}
	if len(admission.slots) != 1 {
		t.Fatalf("active work = %d, want 1", len(admission.slots))
	}
	select {
	case <-inner.finished:
		t.Fatal("uncooperative work stopped before release")
	default:
	}
	busy, err := searcher.Search(t.Context(), searchcore.Request{Query: "busy"})
	if !errors.Is(err, errInteractiveSearchCapacity) || busy.Request.Query != "busy" ||
		len(busy.PartialFailures) != 1 ||
		busy.PartialFailures[0].Reason != interactiveSearchCapacityFailure ||
		inner.calls.Load() != 1 {
		t.Fatalf("busy search = %#v, %v, calls %d", busy, err, inner.calls.Load())
	}

	close(inner.release)
	deadline := time.Now().Add(time.Second)
	for {
		response, searchErr := searcher.Search(
			t.Context(),
			searchcore.Request{Query: "next"},
		)
		if searchErr != nil && !errors.Is(searchErr, errInteractiveSearchCapacity) {
			t.Fatalf("search after release: %v", searchErr)
		}
		if len(response.PartialFailures) == 0 {
			if response.Request.Query != "next" || inner.calls.Load() != 2 {
				t.Fatalf("search after release = %#v, calls %d", response, inner.calls.Load())
			}

			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completed work did not release search admission")
		}
		runtime.Gosched()
	}
}

func TestInteractiveSearchAdmissionRejectsCapacityOrCancellation(t *testing.T) {
	admission := newInteractiveSearchAdmission(1)
	release, err := admission.tryAcquire(t.Context())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if releaseBusy, acquireErr := admission.tryAcquire(t.Context()); releaseBusy != nil ||
		!errors.Is(acquireErr, errInteractiveSearchCapacity) {
		t.Fatalf("saturated acquire = %v, %t", acquireErr, releaseBusy != nil)
	}
	release()
	releaseAgain, err := admission.tryAcquire(t.Context())
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	releaseAgain()

	canceled, cancelCanceled := context.WithCancelCause(t.Context())
	cancelCanceled(context.Canceled)
	if releaseCanceled, acquireErr := admission.tryAcquire(canceled); releaseCanceled != nil ||
		!errors.Is(acquireErr, context.Canceled) {
		t.Fatalf("canceled acquire = %v, %t", acquireErr, releaseCanceled != nil)
	}
}

func TestInteractiveSearchBudgetPreservesParentCancellation(t *testing.T) {
	inner := &countingInteractiveSearch{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response, err := interactiveBudgetFixture(inner, time.Second).Search(
		ctx,
		searchcore.Request{Query: "canceled"},
	)
	if !errors.Is(err, context.Canceled) || len(response.PartialFailures) != 0 ||
		inner.calls.Load() != 0 {
		t.Fatalf("canceled search = %#v, %v, calls %d", response, err, inner.calls.Load())
	}
}

func TestInteractiveSearchBudgetPreservesErrorsAndPanics(t *testing.T) {
	wantErr := errors.New("search failed")
	if _, err := interactiveBudgetFixture(
		errorInteractiveSearch{err: wantErr}, time.Second,
	).Search(t.Context(), searchcore.Request{}); !errors.Is(err, wantErr) {
		t.Fatalf("wrapped error = %v", err)
	}

	wantPanic := "search panic"
	defer func() {
		if recovered := recover(); recovered != wantPanic {
			t.Fatalf("panic = %v", recovered)
		}
	}()
	_, _ = interactiveBudgetFixture(
		panicSearcher{failure: wantPanic}, time.Second,
	).Search(t.Context(), searchcore.Request{})
}

func TestInteractiveSearchBudgetContainsPanicAfterDeadline(t *testing.T) {
	wantPanic := "late search panic"
	inner := &delayedPanicInteractiveSearch{
		started: make(chan struct{}), release: make(chan struct{}), failure: wantPanic,
	}
	admission := newInteractiveSearchAdmission(1)
	reported := make(chan interactiveSearchPanicRecord, 1)
	searcher := interactiveBudgetSearcher{
		inner: inner, budget: time.Hour, grace: time.Minute, admission: admission,
		panicLog: func(_ context.Context, message string, attributes ...any) {
			reported <- interactiveSearchPanicRecord{
				message: message,
				value:   attributes[0].(slog.Attr).Value.Any(),
			}
		},
	}
	ctx, cancel := context.WithCancelCause(t.Context())
	result := make(chan interactiveSearchOutcome, 1)
	go func() {
		response, err := searcher.Search(ctx, searchcore.Request{Query: "panic"})
		result <- interactiveSearchOutcome{response: response, err: err}
	}()
	<-inner.started
	cancel(context.DeadlineExceeded)
	if outcome := <-result; !errors.Is(outcome.err, context.DeadlineExceeded) ||
		len(outcome.response.PartialFailures) != 1 {
		t.Fatalf("deadline response = %#v, %v", outcome.response, outcome.err)
	}
	close(inner.release)

	deadline := time.Now().Add(time.Second)
	for {
		response, err := searcher.Search(t.Context(), searchcore.Request{Query: "after-panic"})
		if err != nil && !errors.Is(err, errInteractiveSearchCapacity) {
			t.Fatalf("search after panic: %v", err)
		}
		if len(response.PartialFailures) == 0 {
			if response.Request.Query != "after-panic" || inner.calls.Load() != 2 {
				t.Fatalf("search after panic = %#v, calls %d", response, inner.calls.Load())
			}

			break
		}
		if time.Now().After(deadline) {
			t.Fatal("late panic did not release search admission")
		}
		runtime.Gosched()
	}
	record := <-reported
	if record.message != interactiveSearchPanicMessage || record.value != wantPanic {
		t.Fatalf("panic log = %#v", record)
	}
}

func TestInteractiveSearchHardDeadlineRendersPortalHTTP200(t *testing.T) {
	inner := &blockingInteractiveSearch{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	portal := publicportal.New(newPortalSource(interactiveBudgetFixture(
		inner,
		20*time.Millisecond,
	)), false)
	response := httptest.NewRecorder()
	portal.ServeHTTP(response, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, "/?q=slow", nil,
	))
	if response.Code != http.StatusOK || !strings.Contains(
		response.Body.String(),
		"Search is temporarily unavailable.",
	) {
		t.Fatalf("portal response = %d %q", response.Code, response.Body.String())
	}
	close(inner.release)
	<-inner.finished
}

func TestInteractiveSearchDeadlineReturnsRecentParsedSession(t *testing.T) {
	stable := searchsession.NewStableWindow(staticSearcher{resp: searchcore.Response{
		TotalResults: 1,
		Results: []searchcore.Result{{
			Title: "DrunkLab", URL: "https://drunklab.example/", Source: searchcore.SourceLocal,
		}},
	}})
	req := searchcore.Request{Query: "drunklab", Limit: 10}
	if _, err := withParsedQuery(stable).Search(t.Context(), req); err != nil {
		t.Fatal(err)
	}

	inner := &blockingInteractiveSearch{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	searcher := withParsedQuery(searchsession.WithRecentSuccessOnIncompleteRefresh(
		interactiveBudgetFixture(inner, 20*time.Millisecond),
		stable,
	))
	response, err := searcher.Search(t.Context(), req)
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != "https://drunklab.example/" ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Source != interactiveSearchFailureSource {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	close(inner.release)
	select {
	case <-inner.finished:
	case <-time.After(time.Second):
		t.Fatal("interactive search did not finish")
	}
}

type panicSearcher struct {
	failure any
}

type errorInteractiveSearch struct {
	err error
}

func (s errorInteractiveSearch) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	return searchcore.Response{}, s.err
}

func (s panicSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	panic(s.failure)
}
