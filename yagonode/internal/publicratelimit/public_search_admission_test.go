package publicratelimit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestWrapCapsConcurrentExpensiveSearches(t *testing.T) {
	limiter, _ := scriptedLimiter(Tiers{Per3Seconds: 100, PerMinute: 100, Per10Minutes: 100})
	started := make(chan struct{}, maximumConcurrentPublicSearches)
	releaseHandlers := make(chan struct{})
	var active atomic.Int64
	var maximumActive atomic.Int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if classifyPublicSearchRequest(r) != expensivePublicSearchRequest {
			w.WriteHeader(http.StatusOK)

			return
		}
		current := active.Add(1)
		for observed := maximumActive.Load(); current > observed; observed = maximumActive.Load() {
			if maximumActive.CompareAndSwap(observed, current) {
				break
			}
		}
		started <- struct{}{}
		<-releaseHandlers
		active.Add(-1)
		w.WriteHeader(http.StatusOK)
	})
	handler := Wrap(next, limiter, nil)
	var requests sync.WaitGroup
	for i := 0; i < maximumConcurrentPublicSearches; i++ {
		requests.Add(1)
		go func(client int) {
			defer requests.Done()
			req := httptest.NewRequestWithContext(
				t.Context(),
				http.MethodGet,
				"/?q=bounded",
				nil,
			)
			req.RemoteAddr = fmt.Sprintf("203.0.113.%d:1", client+1)
			handler.ServeHTTP(httptest.NewRecorder(), req)
		}(i)
	}
	for range maximumConcurrentPublicSearches {
		<-started
	}
	overflow := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=overflow", nil)
	overflow.RemoteAddr = "198.51.100.1:1"
	overflowResult := httptest.NewRecorder()
	handler.ServeHTTP(overflowResult, overflow)
	if overflowResult.Code != http.StatusServiceUnavailable ||
		overflowResult.Header().Get("Retry-After") != "1" {
		t.Fatalf(
			"overflow = %d retry=%q",
			overflowResult.Code,
			overflowResult.Header().Get("Retry-After"),
		)
	}
	click := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/searchclick", nil)
	click.RemoteAddr = "198.51.100.2:1"
	clickResult := httptest.NewRecorder()
	handler.ServeHTTP(clickResult, click)
	if clickResult.Code != http.StatusOK {
		t.Fatalf("search click = %d", clickResult.Code)
	}
	tavily := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/search", nil)
	tavily.RemoteAddr = "198.51.100.3:1"
	tavilyResult := httptest.NewRecorder()
	handler.ServeHTTP(tavilyResult, tavily)
	if tavilyResult.Code != http.StatusOK {
		t.Fatalf("deferred Tavily search = %d", tavilyResult.Code)
	}
	close(releaseHandlers)
	requests.Wait()
	if got := maximumActive.Load(); got != maximumConcurrentPublicSearches {
		t.Fatalf("maximum active = %d, want %d", got, maximumConcurrentPublicSearches)
	}
	if retained := len(publicSearchAdmission); retained != 0 {
		t.Fatalf("retained admissions = %d", retained)
	}
}

func TestWrapChecksRateBeforeAdmission(t *testing.T) {
	for i := 0; i < maximumConcurrentPublicSearches; i++ {
		publicSearchAdmission <- struct{}{}
	}
	defer func() {
		for range maximumConcurrentPublicSearches {
			<-publicSearchAdmission
		}
	}()
	limiter, _ := scriptedLimiter(Tiers{Per3Seconds: 1, PerMinute: 1, Per10Minutes: 1})
	handler := Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("saturated handler must not run")
	}), limiter, nil)
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?q=rate", nil)
		req.RemoteAddr = "203.0.113.99:1"
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)

		return result
	}
	if result := request(); result.Code != http.StatusServiceUnavailable {
		t.Fatalf("first request = %d", result.Code)
	}
	if result := request(); result.Code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d", result.Code)
	}
}

func TestWrapReleasesCanceledSearch(t *testing.T) {
	limiter, _ := scriptedLimiter(Tiers{Per3Seconds: 100, PerMinute: 100, Per10Minutes: 100})
	started := make(chan struct{})
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	handler := Wrap(next, limiter, nil)
	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/?q=cancel", nil)
	req.RemoteAddr = "203.0.113.12:1"
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()
	<-started
	cancel()
	<-done
	if retained := len(publicSearchAdmission); retained != 0 {
		t.Fatalf("retained admissions after cancellation = %d", retained)
	}
}

func TestWrapDefersTavilySearchPreflight(t *testing.T) {
	limiter, _ := scriptedLimiter(Tiers{Per3Seconds: 1, PerMinute: 1, Per10Minutes: 1})
	authenticationCalls := 0
	handler := Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), limiter, func(*http.Request) bool {
		authenticationCalls++

		return true
	})
	for range 2 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/search", nil)
		req.RemoteAddr = "203.0.113.13:1"
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		if result.Code != http.StatusOK {
			t.Fatalf("Tavily preflight = %d", result.Code)
		}
	}
	if authenticationCalls != 0 || len(limiter.clients) != 0 {
		t.Fatalf(
			"preflight auth calls = %d clients = %d",
			authenticationCalls,
			len(limiter.clients),
		)
	}
}
