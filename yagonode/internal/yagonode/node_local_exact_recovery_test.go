package yagonode

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type localExactCountingSearcher struct {
	response searchcore.Response
	err      error
	calls    int
}

func (s *localExactCountingSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	s.calls++

	return s.response, s.err
}

func TestLocalExactRecoveryKeepsKnownLocalResultAfterFederatedFailure(t *testing.T) {
	req := searchcore.Request{Query: "drunklab", Terms: []string{"drunklab"}, Limit: 10}
	primary := &localExactCountingSearcher{response: webFallbackExactStageFailure(
		req,
		webFallbackExactStageTimeoutFailure,
	)}
	local := &localExactCountingSearcher{response: searchcore.Response{
		Results: []searchcore.Result{{URL: "https://drunklab.example/"}},
		Facets:  []searchcore.FacetGroup{{Name: "host"}},
		PartialFailures: []searchcore.PartialFailure{{
			Source: "local-evidence", Reason: "partial",
		}},
	}}
	response, err := withLocalExactRecovery(primary, local).Search(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if primary.calls != 1 || local.calls != 1 || len(response.Results) != 1 ||
		response.TotalResults != 1 || response.Request.Query != req.Query ||
		len(response.PartialFailures) != 2 || len(response.Facets) != 1 {
		t.Fatalf(
			"response = %#v, primary calls = %d, local calls = %d",
			response,
			primary.calls,
			local.calls,
		)
	}
}

func TestLocalExactRecoveryOnlyRunsForEmptyExactStageFailure(t *testing.T) {
	tests := []searchcore.Response{
		{Results: []searchcore.Result{{URL: "https://local.example/"}}},
		{},
	}
	for _, primaryResponse := range tests {
		primary := &localExactCountingSearcher{response: primaryResponse}
		local := &localExactCountingSearcher{}
		response, err := withLocalExactRecovery(primary, local).Search(
			t.Context(),
			searchcore.Request{Query: "query"},
		)
		if err != nil || local.calls != 0 || len(response.Results) != len(primaryResponse.Results) {
			t.Fatalf("response = %#v, error = %v, local calls = %d", response, err, local.calls)
		}
	}

	want := errors.New("primary failed")
	primary := &localExactCountingSearcher{err: want}
	local := &localExactCountingSearcher{}
	_, err := withLocalExactRecovery(primary, local).Search(
		t.Context(),
		searchcore.Request{Query: "query"},
	)
	if !errors.Is(err, want) || local.calls != 0 {
		t.Fatalf("error = %v, local calls = %d", err, local.calls)
	}
}

func TestLocalExactRecoveryMissPreservesAllFailures(t *testing.T) {
	primary := &localExactCountingSearcher{response: webFallbackExactStageFailure(
		searchcore.Request{Query: "missing"},
		webFallbackExactStageCapacityFailure,
	)}
	local := &localExactCountingSearcher{response: searchcore.Response{
		PartialFailures: []searchcore.PartialFailure{{
			Source: localExactRecoveryFailureSource,
			Reason: localExactRecoveryTimeoutFailure,
		}},
	}}
	response, err := withLocalExactRecovery(primary, local).Search(
		t.Context(),
		searchcore.Request{Query: "missing"},
	)
	if err != nil || len(response.Results) != 0 || len(response.PartialFailures) != 2 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestLocalExactRecoveryWrapsCanceledRetry(t *testing.T) {
	primary := &localExactCountingSearcher{response: webFallbackExactStageFailure(
		searchcore.Request{Query: "cancel"},
		webFallbackExactStageTimeoutFailure,
	)}
	local := &localExactCountingSearcher{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := withLocalExactRecovery(primary, local).Search(
		ctx,
		searchcore.Request{Query: "cancel"},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestLocalExactRecoveryStageBoundsUncooperativeWork(t *testing.T) {
	inner := &blockingInteractiveSearch{
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	admission := newInteractiveSearchAdmission(1)
	searcher := recoveryBudgetSearcher{
		inner: inner, budget: 20 * time.Millisecond, grace: 10 * time.Millisecond,
		admission: admission, panicLog: discardInteractiveSearchPanic,
		profile: localExactRecoveryProfile,
	}
	response, err := searcher.Search(t.Context(), searchcore.Request{Query: "slow"})
	if err != nil || len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Source != localExactRecoveryFailureSource ||
		response.PartialFailures[0].Reason != localExactRecoveryTimeoutFailure {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	busy, err := searcher.Search(t.Context(), searchcore.Request{Query: "busy"})
	if err != nil || len(busy.PartialFailures) != 1 ||
		busy.PartialFailures[0].Reason != localExactRecoveryCapacityFailure ||
		inner.calls.Load() != 1 {
		t.Fatalf("busy = %#v, error = %v, calls = %d", busy, err, inner.calls.Load())
	}
	close(inner.release)
	select {
	case <-inner.finished:
	case <-time.After(time.Second):
		t.Fatal("local exact work did not finish")
	}
}

func TestLocalExactRecoveryStageForwardsConfiguredPanic(t *testing.T) {
	reported := make(chan interactiveSearchPanicRecord, 1)
	searcher := recoveryBudgetSearcher{
		inner:  panicSearcher{failure: "local panic"},
		budget: time.Second, grace: time.Millisecond,
		admission: newInteractiveSearchAdmission(1), profile: localExactRecoveryProfile,
		panicLog: func(_ context.Context, message string, attributes ...any) {
			reported <- interactiveSearchPanicRecord{
				message: message,
				value:   attributes[0].(slog.Attr).Value.Any(),
			}
		},
	}
	defer func() {
		if recover() != "local panic" {
			t.Fatal("local panic was not forwarded")
		}
		record := <-reported
		if record.message != localExactRecoveryPanicMessage || record.value != "local panic" {
			t.Fatalf("panic record = %#v", record)
		}
	}()
	_, _ = searcher.Search(t.Context(), searchcore.Request{Query: "panic"})
}

func TestLocalExactRecoveryBudgetUsesProductionProfile(t *testing.T) {
	previousBudget := localExactRecoveryBudget
	previousAdmission := processLocalExactRecoveryAdmission
	localExactRecoveryBudget = 20 * time.Millisecond
	processLocalExactRecoveryAdmission = newInteractiveSearchAdmission(1)
	t.Cleanup(func() {
		localExactRecoveryBudget = previousBudget
		processLocalExactRecoveryAdmission = previousAdmission
	})
	response, err := withLocalExactRecoveryBudget(errorInteractiveSearch{
		err: errors.New("broken"),
	}).Search(t.Context(), searchcore.Request{Query: "broken"})
	if err != nil || len(response.PartialFailures) != 1 ||
		response.PartialFailures[0] != (searchcore.PartialFailure{
			Source: localExactRecoveryFailureSource,
			Reason: localExactRecoveryFailed,
		}) {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}
