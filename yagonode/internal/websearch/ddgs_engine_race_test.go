package websearch

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEngineRaceReturnsFirstAcceptedAnswer(t *testing.T) {
	var thirdCalls atomic.Int32
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "third.example" {
			thirdCalls.Add(1)
		}

		return htmlResponse(http.StatusOK, "answer"), nil
	})
	client := &http.Client{Transport: transport}
	provider := NewDDGSProvider(DDGSConfig{
		Client: client,
		Accept: VerifiedForQuery,
		Now:    fixedClock(),
	})
	provider.engines = []engine{
		tracingEngine("first", Result{Title: "unrelated", URL: "https://unrelated.example"}),
		tracingEngine("second", Result{Title: "needle", URL: "https://second.example"}),
		tracingEngine("third", Result{Title: "needle", URL: "https://third.example"}),
	}

	results, err := provider.Search(t.Context(), "needle", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://second.example" {
		t.Fatalf("results = %#v", results)
	}
	if thirdCalls.Load() != 0 {
		t.Fatalf("third engine calls = %d, want 0", thirdCalls.Load())
	}
}

func TestEngineRaceCancelsSlowPreferredEngine(t *testing.T) {
	canceled := make(chan struct{})
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host == "slow.example" {
				<-request.Context().Done()
				close(canceled)

				return nil, request.Context().Err()
			}

			return htmlResponse(http.StatusOK, "answer"), nil
		}),
	}
	provider := NewDDGSProvider(DDGSConfig{Client: client, Now: fixedClock()})
	provider.engines = []engine{
		tracingEngine("slow", Result{Title: "slow", URL: "https://slow.example"}),
		tracingEngine("fast", Result{Title: "fast", URL: "https://fast.example"}),
	}

	results, err := provider.Search(t.Context(), "fast", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://fast.example" {
		t.Fatalf("results = %#v", results)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("slow engine did not observe cancellation")
	}
}

func TestEngineRacePreservesPerEngineBackoff(t *testing.T) {
	var limitedCalls atomic.Int32
	var answerCalls atomic.Int32
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Host == "limited.example" {
				limitedCalls.Add(1)

				return htmlResponse(http.StatusTooManyRequests, ""), nil
			}
			answerCalls.Add(1)

			return htmlResponse(http.StatusOK, "answer"), nil
		}),
	}
	provider := NewDDGSProvider(DDGSConfig{Client: client, Now: fixedClock()})
	provider.engines = []engine{
		tracingEngine("limited", Result{}),
		tracingEngine("answer", Result{Title: "answer", URL: "https://answer.example"}),
	}

	for _, query := range []string{"first", "second"} {
		results, err := provider.Search(t.Context(), query, 10)
		if err != nil {
			t.Fatalf("search(%q): %v", query, err)
		}
		if len(results) != 1 {
			t.Fatalf("search(%q) results = %#v", query, results)
		}
	}
	if limitedCalls.Load() != 1 {
		t.Fatalf("limited engine calls = %d, want 1", limitedCalls.Load())
	}
	if answerCalls.Load() != 2 {
		t.Fatalf("answer engine calls = %d, want 2", answerCalls.Load())
	}
}

func TestEngineRaceReturnsDeterministicLastFailure(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			status := http.StatusBadGateway
			if request.URL.Host == "last.example" {
				status = http.StatusServiceUnavailable
			}

			return htmlResponse(status, ""), nil
		}),
	}
	provider := NewDDGSProvider(DDGSConfig{Client: client, Now: fixedClock()})
	provider.engines = []engine{
		tracingEngine("first", Result{}),
		tracingEngine("last", Result{}),
	}

	_, err := provider.Search(t.Context(), "failure", 10)
	if err == nil || !strings.Contains(err.Error(), "last status 503") {
		t.Fatalf("error = %v, want last engine failure", err)
	}
}

func TestEngineRaceStopsAtCanceledParentContext(t *testing.T) {
	release := make(chan struct{})
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			<-release

			return nil, request.Context().Err()
		}),
	}
	provider := NewDDGSProvider(DDGSConfig{Client: client, Backend: backendMojeek})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := provider.Search(ctx, "canceled", 10)
	close(release)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestEngineRaceOrdersReadyAttemptsByPreference(t *testing.T) {
	scheduler := &engineRace{attempts: make(chan engineAttempt, 1)}
	scheduler.attempts <- engineAttempt{preference: 0}

	ready := scheduler.drainReady(engineAttempt{preference: 1})
	if len(ready) != 2 || ready[0].preference != 0 || ready[1].preference != 1 {
		t.Fatalf("ready attempts = %#v", ready)
	}
}

func tracingEngine(name string, result Result) engine {
	return engine{
		name:     name,
		endpoint: "https://" + name + ".example/search",
		queryKey: "q",
		parse: func([]byte) ([]Result, error) {
			return []Result{result}, nil
		},
		safe: noSafeParams,
	}
}
