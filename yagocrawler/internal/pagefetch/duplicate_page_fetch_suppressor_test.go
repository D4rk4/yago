package pagefetch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

type pageFetchResult struct {
	page FetchedPage
	err  error
}

type blockingPageSource struct {
	started chan struct{}
	release chan pageFetchResult
	calls   atomic.Int64
}

func newBlockingPageSource() *blockingPageSource {
	return &blockingPageSource{
		started: make(chan struct{}, 8),
		release: make(chan pageFetchResult, 8),
	}
}

func (s *blockingPageSource) Fetch(
	ctx context.Context,
	_ *url.URL,
) (FetchedPage, error) {
	s.calls.Add(1)
	s.started <- struct{}{}
	select {
	case result := <-s.release:
		return result.page, result.err
	case <-ctx.Done():
		return FetchedPage{}, fmt.Errorf("blocking page fetch: %w", ctx.Err())
	}
}

func parseFetchTarget(t testing.TB, raw string) *url.URL {
	t.Helper()
	target, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse fetch target: %v", err)
	}

	return target
}

func waitForFetchParticipants(
	t *testing.T,
	suppressor *DuplicatePageFetchSuppressor,
	want int,
) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		suppressor.mu.Lock()
		participants := 0
		for _, flight := range suppressor.inflight {
			participants += flight.participants
		}
		suppressor.mu.Unlock()
		if participants == want {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("in-flight participants did not reach %d", want)
}

func fetchAsync(
	ctx context.Context,
	suppressor *DuplicatePageFetchSuppressor,
	target *url.URL,
) <-chan pageFetchResult {
	result := make(chan pageFetchResult, 1)
	go func() {
		page, err := suppressor.Fetch(ctx, target)
		result <- pageFetchResult{page: page, err: err}
	}()

	return result
}

func TestDuplicatePageFetchSuppressorCoalescesAndClones(t *testing.T) {
	inner := newBlockingPageSource()
	suppressor := NewDuplicatePageFetchSuppressor(inner)
	target := parseFetchTarget(t, "https://example.test/page?q=1")
	results := make([]<-chan pageFetchResult, 0, 3)
	results = append(results, fetchAsync(t.Context(), suppressor, target))
	<-inner.started
	results = append(
		results,
		fetchAsync(t.Context(), suppressor, target),
		fetchAsync(t.Context(), suppressor, target),
	)
	waitForFetchParticipants(t, suppressor, 3)
	original := FetchedPage{URL: target, ContentType: "text/html", Body: []byte("page")}
	inner.release <- pageFetchResult{page: original}

	pages := make([]FetchedPage, 0, len(results))
	for _, result := range results {
		resolved := <-result
		if resolved.err != nil {
			t.Fatalf("fetch error: %v", resolved.err)
		}
		pages = append(pages, resolved.page)
	}
	if inner.calls.Load() != 1 {
		t.Fatalf("underlying fetches = %d, want 1", inner.calls.Load())
	}
	pages[0].Body[0] = 'P'
	pages[0].URL.Path = "/changed"
	if string(pages[1].Body) != "page" || pages[1].URL.Path != "/page" {
		t.Fatalf("second result shares mutable state: %+v", pages[1])
	}
	if string(pages[2].Body) != "page" || pages[2].URL.Path != "/page" {
		t.Fatalf("third result shares mutable state: %+v", pages[2])
	}
	if string(original.Body) != "page" || original.URL.Path != "/page" {
		t.Fatalf("underlying result was mutated: %+v", original)
	}
}

func TestDuplicatePageFetchSuppressorSeparatesBrowserModes(t *testing.T) {
	inner := newBlockingPageSource()
	suppressor := NewDuplicatePageFetchSuppressor(inner)
	target := parseFetchTarget(t, "https://example.test/page")
	enabled := fetchAsync(t.Context(), suppressor, target)
	<-inner.started
	disabled := fetchAsync(WithoutBrowserFallback(t.Context()), suppressor, target)
	<-inner.started
	inner.release <- pageFetchResult{page: FetchedPage{URL: target, Body: []byte("enabled")}}
	inner.release <- pageFetchResult{page: FetchedPage{URL: target, Body: []byte("disabled")}}
	if result := <-enabled; result.err != nil {
		t.Fatalf("browser-enabled fetch: %v", result.err)
	}
	if result := <-disabled; result.err != nil {
		t.Fatalf("browser-disabled fetch: %v", result.err)
	}
	if inner.calls.Load() != 2 {
		t.Fatalf("underlying fetches = %d, want 2", inner.calls.Load())
	}
}

func TestDuplicatePageFetchSuppressorSeparatesChains(t *testing.T) {
	inner := newBlockingPageSource()
	first := NewDuplicatePageFetchSuppressor(inner)
	second := NewDuplicatePageFetchSuppressor(inner)
	target := parseFetchTarget(t, "https://example.test/page")
	firstResult := fetchAsync(t.Context(), first, target)
	secondResult := fetchAsync(t.Context(), second, target)
	<-inner.started
	<-inner.started
	inner.release <- pageFetchResult{}
	inner.release <- pageFetchResult{}
	if result := <-firstResult; result.err != nil {
		t.Fatalf("first chain: %v", result.err)
	}
	if result := <-secondResult; result.err != nil {
		t.Fatalf("second chain: %v", result.err)
	}
	if inner.calls.Load() != 2 {
		t.Fatalf("underlying fetches = %d, want 2", inner.calls.Load())
	}
}

func TestDuplicatePageFetchSuppressorSeparatesExactTargets(t *testing.T) {
	inner := newBlockingPageSource()
	suppressor := NewDuplicatePageFetchSuppressor(inner)
	firstTarget := parseFetchTarget(t, "https://example.test/page?q=1")
	secondTarget := parseFetchTarget(t, "https://example.test/page?q=2")
	first := fetchAsync(t.Context(), suppressor, firstTarget)
	second := fetchAsync(t.Context(), suppressor, secondTarget)
	<-inner.started
	<-inner.started
	inner.release <- pageFetchResult{}
	inner.release <- pageFetchResult{}
	if result := <-first; result.err != nil {
		t.Fatalf("first target: %v", result.err)
	}
	if result := <-second; result.err != nil {
		t.Fatalf("second target: %v", result.err)
	}
	if inner.calls.Load() != 2 {
		t.Fatalf("underlying fetches = %d, want 2", inner.calls.Load())
	}
}

func TestDuplicatePageFetchSuppressorReleasesErrorForRetry(t *testing.T) {
	inner := newBlockingPageSource()
	suppressor := NewDuplicatePageFetchSuppressor(inner)
	target := parseFetchTarget(t, "https://example.test/page")
	first := fetchAsync(t.Context(), suppressor, target)
	<-inner.started
	second := fetchAsync(t.Context(), suppressor, target)
	waitForFetchParticipants(t, suppressor, 2)
	sentinel := errors.New("temporary")
	inner.release <- pageFetchResult{err: sentinel}
	for _, result := range []<-chan pageFetchResult{first, second} {
		if resolved := <-result; !errors.Is(resolved.err, sentinel) {
			t.Fatalf("coalesced error = %v, want %v", resolved.err, sentinel)
		}
	}

	retry := fetchAsync(t.Context(), suppressor, target)
	<-inner.started
	inner.release <- pageFetchResult{page: FetchedPage{URL: target, Body: []byte("retry")}}
	if result := <-retry; result.err != nil || string(result.page.Body) != "retry" {
		t.Fatalf("retry result = %+v", result)
	}
	if inner.calls.Load() != 2 {
		t.Fatalf("underlying fetches = %d, want 2", inner.calls.Load())
	}
}

func TestDuplicatePageFetchSuppressorLetsCancelledFollowerLeave(t *testing.T) {
	inner := newBlockingPageSource()
	suppressor := NewDuplicatePageFetchSuppressor(inner)
	target := parseFetchTarget(t, "https://example.test/page")
	leader := fetchAsync(t.Context(), suppressor, target)
	<-inner.started
	followerContext, cancelFollower := context.WithCancel(t.Context())
	follower := fetchAsync(followerContext, suppressor, target)
	waitForFetchParticipants(t, suppressor, 2)
	cancelFollower()
	if result := <-follower; !errors.Is(result.err, context.Canceled) {
		t.Fatalf("follower error = %v, want cancellation", result.err)
	}
	select {
	case result := <-leader:
		t.Fatalf("leader returned before fetch release: %+v", result)
	default:
	}
	inner.release <- pageFetchResult{page: FetchedPage{URL: target}}
	if result := <-leader; result.err != nil {
		t.Fatalf("leader error: %v", result.err)
	}
}

func TestDuplicatePageFetchSuppressorShutdownReleasesAll(t *testing.T) {
	inner := newBlockingPageSource()
	suppressor := NewDuplicatePageFetchSuppressor(inner)
	target := parseFetchTarget(t, "https://example.test/page")
	fetchContext, cancelFetch := context.WithCancel(t.Context())
	leader := fetchAsync(fetchContext, suppressor, target)
	<-inner.started
	follower := fetchAsync(fetchContext, suppressor, target)
	waitForFetchParticipants(t, suppressor, 2)
	cancelFetch()
	for _, result := range []<-chan pageFetchResult{leader, follower} {
		if resolved := <-result; !errors.Is(resolved.err, context.Canceled) {
			t.Fatalf("shutdown error = %v, want cancellation", resolved.err)
		}
	}
	suppressor.mu.Lock()
	remaining := len(suppressor.inflight)
	suppressor.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("remaining in-flight fetches = %d, want 0", remaining)
	}
}

func TestDuplicatePageFetchSuppressorRejectsAlreadyCancelledCall(t *testing.T) {
	inner := newBlockingPageSource()
	suppressor := NewDuplicatePageFetchSuppressor(inner)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := suppressor.Fetch(
		ctx,
		parseFetchTarget(t, "https://example.test/page"),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("fetch error = %v, want cancellation", err)
	}
	if inner.calls.Load() != 0 {
		t.Fatalf("underlying fetches = %d, want 0", inner.calls.Load())
	}
}

func BenchmarkDuplicatePageFetchSuppressor(b *testing.B) {
	target := parseFetchTarget(b, "https://example.test/page")
	suppressor := NewDuplicatePageFetchSuppressor(
		pageSourceFunction(func(context.Context, *url.URL) (FetchedPage, error) {
			return FetchedPage{URL: target, Body: []byte("page")}, nil
		}),
	)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := suppressor.Fetch(context.Background(), target); err != nil {
			b.Fatal(err)
		}
	}
}

type pageSourceFunction func(context.Context, *url.URL) (FetchedPage, error)

func (f pageSourceFunction) Fetch(
	ctx context.Context,
	target *url.URL,
) (FetchedPage, error) {
	return f(ctx, target)
}
