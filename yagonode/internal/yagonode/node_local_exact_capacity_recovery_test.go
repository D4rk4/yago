package yagonode

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/websearch"
)

type delayedLocalExactResult struct {
	delay time.Duration
}

type capacityRecoveryWebProvider struct {
	calls int
}

func (p *capacityRecoveryWebProvider) Search(
	context.Context,
	string,
	int,
) ([]websearch.Result, error) {
	p.calls++

	return []websearch.Result{{
		Title: "Check Point API",
		URL:   "https://checkpoint.example/api",
	}}, nil
}

func (s delayedLocalExactResult) Search(
	ctx context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	select {
	case <-time.After(s.delay):
		return searchcore.Response{
			Results:      []searchcore.Result{{URL: "https://checkpoint.example/"}},
			TotalResults: 1,
		}, nil
	case <-ctx.Done():
		return searchcore.Response{}, fmt.Errorf(
			"wait for delayed local exact result: %w",
			context.Cause(ctx),
		)
	}
}

func TestLocalExactCapacityRecoveryUsesExtendedBoundedBudget(t *testing.T) {
	previousAdmission := processLocalExactRecoveryAdmission
	processLocalExactRecoveryAdmission = newInteractiveSearchAdmission(1)
	t.Cleanup(func() { processLocalExactRecoveryAdmission = previousAdmission })

	request := searchcore.Request{Query: "check point api"}
	primary := &localExactCountingSearcher{response: webFallbackExactStageFailure(
		request,
		webFallbackExactStageCapacityFailure,
	)}
	response, err := withLocalExactRecovery(
		primary,
		delayedLocalExactResult{delay: 200 * time.Millisecond},
	).Search(t.Context(), request)
	if err != nil || len(response.Results) != 1 || response.Results[0].URL == "" {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestLocalExactCapacityRecoveryWaitsForRetryAdmission(t *testing.T) {
	previousAdmission := processLocalExactRecoveryAdmission
	processLocalExactRecoveryAdmission = newInteractiveSearchAdmission(1)
	t.Cleanup(func() { processLocalExactRecoveryAdmission = previousAdmission })

	release, err := processLocalExactRecoveryAdmission.tryAcquire(t.Context())
	if err != nil {
		t.Fatalf("occupy recovery admission: %v", err)
	}
	request := searchcore.Request{Query: "check point api"}
	primary := &localExactCountingSearcher{response: webFallbackExactStageFailure(
		request,
		webFallbackExactStageCapacityFailure,
	)}
	outcome := make(chan interactiveSearchOutcome, 1)
	go func() {
		response, searchErr := withLocalExactRecovery(
			primary,
			delayedLocalExactResult{},
		).Search(t.Context(), request)
		outcome <- interactiveSearchOutcome{response: response, err: searchErr}
	}()
	select {
	case result := <-outcome:
		t.Fatalf("capacity recovery did not wait: %#v, %v", result.response, result.err)
	case <-time.After(10 * time.Millisecond):
	}
	release()
	select {
	case result := <-outcome:
		if result.err != nil || len(result.response.Results) != 1 ||
			result.response.Results[0].URL == "" {
			t.Fatalf("capacity recovery = %#v, %v", result.response, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("capacity recovery did not acquire released admission")
	}
}

func TestLocalExactCapacityAdmissionTimeoutContinuesToWebFallback(t *testing.T) {
	admission := newInteractiveSearchAdmission(1)
	release, err := admission.tryAcquire(t.Context())
	if err != nil {
		t.Fatalf("occupy recovery admission: %v", err)
	}
	defer release()

	provider := &capacityRecoveryWebProvider{}
	primary := recoveryBudgetSearcher{
		inner:            &countingInteractiveSearch{},
		budget:           20 * time.Millisecond,
		grace:            5 * time.Millisecond,
		admission:        admission,
		waitForAdmission: true,
		panicLog:         discardInteractiveSearchPanic,
		profile:          localExactRecoveryProfile,
	}
	searcher := websearch.NewFallbackSearcher(
		primary,
		provider,
		func(searchcore.Request) bool { return true },
	)
	response, err := searcher.Search(t.Context(), searchcore.Request{
		Query: "check point api",
		Limit: 10,
	})
	if err != nil || provider.calls != 1 || len(response.Results) != 1 ||
		response.Results[0].URL != "https://checkpoint.example/api" ||
		!hasPartialFailure(
			response.PartialFailures,
			localExactRecoveryFailureSource,
			localExactRecoveryTimeoutFailure,
		) {
		t.Fatalf(
			"response = %#v, provider calls = %d, error = %v",
			response,
			provider.calls,
			err,
		)
	}
}

func TestLocalExactCapacityAdmissionPreservesParentDeadline(t *testing.T) {
	admission := newInteractiveSearchAdmission(1)
	release, err := admission.tryAcquire(t.Context())
	if err != nil {
		t.Fatalf("occupy recovery admission: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	response, err := (recoveryBudgetSearcher{
		inner:            &countingInteractiveSearch{},
		budget:           time.Second,
		grace:            50 * time.Millisecond,
		admission:        admission,
		waitForAdmission: true,
		panicLog:         discardInteractiveSearchPanic,
		profile:          localExactRecoveryProfile,
	}).Search(ctx, searchcore.Request{Query: "check point api"})
	if !errors.Is(err, context.DeadlineExceeded) || len(response.PartialFailures) != 0 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestLocalExactNonCapacityRecoveryRetainsOrdinaryBudget(t *testing.T) {
	previousAdmission := processLocalExactRecoveryAdmission
	processLocalExactRecoveryAdmission = newInteractiveSearchAdmission(1)
	t.Cleanup(func() { processLocalExactRecoveryAdmission = previousAdmission })

	request := searchcore.Request{Query: "bounded retry"}
	for _, response := range []searchcore.Response{
		webFallbackExactStageFailure(request, webFallbackExactStageTimeoutFailure),
		{
			PartialFailures: []searchcore.PartialFailure{{
				Source: webFallbackExactStageFailureSource,
				Reason: "unrelated",
			}},
		},
	} {
		started := time.Now()
		result, err := withLocalExactRecoveryBudgetForFailure(
			delayedLocalExactResult{delay: 200 * time.Millisecond},
			response,
		).Search(t.Context(), request)
		if err != nil || len(result.Results) != 0 ||
			!hasPartialFailure(
				result.PartialFailures,
				localExactRecoveryFailureSource,
				localExactRecoveryTimeoutFailure,
			) || time.Since(started) >= localExactCapacityRecoveryBudget {
			t.Fatalf("response = %#v, error = %v", result, err)
		}
	}
}
