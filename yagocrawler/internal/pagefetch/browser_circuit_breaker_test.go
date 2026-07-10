package pagefetch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"
)

type breakerStubSource struct {
	err   error
	calls int
}

func (s *breakerStubSource) Fetch(_ context.Context, target *url.URL) (FetchedPage, error) {
	s.calls++
	if s.err != nil {
		return FetchedPage{}, s.err
	}
	return FetchedPage{URL: target}, nil
}

type breakerClock struct{ t time.Time }

func (c *breakerClock) now() time.Time          { return c.t }
func (c *breakerClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func breakerTestURL(t *testing.T) *url.URL {
	t.Helper()
	parsed, err := url.Parse("http://example.com/")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	return parsed
}

func newTestBreaker(
	inner PageSource,
	threshold int,
	clock *breakerClock,
) *BrowserCircuitBreaker {
	return &BrowserCircuitBreaker{
		inner:     inner,
		threshold: threshold,
		cooldown:  time.Minute,
		now:       clock.now,
	}
}

func TestBrowserCircuitBreakerOpensAfterThreshold(t *testing.T) {
	malfunction := errors.New("chrome failed to start")
	inner := &breakerStubSource{err: malfunction}
	breaker := newTestBreaker(inner, 3, &breakerClock{t: time.Unix(1_000_000, 0)})
	target := breakerTestURL(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := breaker.Fetch(ctx, target); !errors.Is(err, malfunction) {
			t.Fatalf("fetch %d error = %v, want the browser malfunction", i, err)
		}
	}

	_, err := breaker.Fetch(ctx, target)
	if !errors.Is(err, ErrPageRejected) {
		t.Fatalf("open-circuit error = %v, want a page rejection", err)
	}
	if errors.Is(err, malfunction) {
		t.Fatal("an open circuit must not call the browser")
	}
	if inner.calls != 3 {
		t.Fatalf("browser called %d times, want 3 (open circuit stops calling it)", inner.calls)
	}
}

func TestBrowserCircuitBreakerSuccessResetsFailureRun(t *testing.T) {
	inner := &breakerStubSource{}
	breaker := newTestBreaker(inner, 3, &breakerClock{t: time.Unix(1_000_000, 0)})
	target := breakerTestURL(t)
	ctx := context.Background()

	inner.err = errors.New("boom")
	_, _ = breaker.Fetch(ctx, target)
	_, _ = breaker.Fetch(ctx, target)

	inner.err = nil
	if _, err := breaker.Fetch(ctx, target); err != nil {
		t.Fatalf("healthy fetch: %v", err)
	}

	// Two fresh failures after the reset must not open the breaker (the run
	// restarted), so both still reach the browser.
	inner.err = errors.New("boom")
	callsBefore := inner.calls
	_, _ = breaker.Fetch(ctx, target)
	_, _ = breaker.Fetch(ctx, target)
	if inner.calls != callsBefore+2 {
		t.Fatalf(
			"browser called %d extra times, want 2 (breaker stayed closed)",
			inner.calls-callsBefore,
		)
	}
}

func TestBrowserCircuitBreakerContentRejectionStaysClosed(t *testing.T) {
	inner := &breakerStubSource{
		err: fmt.Errorf("browser fetch content type: %w", ErrUnsupportedContentType),
	}
	breaker := newTestBreaker(inner, 2, &breakerClock{t: time.Unix(1_000_000, 0)})
	target := breakerTestURL(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _ = breaker.Fetch(ctx, target)
	}
	if inner.calls != 5 {
		t.Fatalf(
			"browser called %d times, want 5 (a content rejection proves it is alive)",
			inner.calls,
		)
	}
}

func TestBrowserCircuitBreakerHalfOpenClosesOnSuccess(t *testing.T) {
	clock := &breakerClock{t: time.Unix(1_000_000, 0)}
	inner := &breakerStubSource{err: errors.New("boom")}
	breaker := newTestBreaker(inner, 2, clock)
	target := breakerTestURL(t)
	ctx := context.Background()

	_, _ = breaker.Fetch(ctx, target)
	_, _ = breaker.Fetch(ctx, target) // opens
	callsAtOpen := inner.calls

	// Within the cooldown the breaker short-circuits.
	_, _ = breaker.Fetch(ctx, target)
	if inner.calls != callsAtOpen {
		t.Fatal("breaker should short-circuit within the cooldown")
	}

	// After the cooldown a probe is admitted; make it succeed.
	clock.advance(2 * time.Minute)
	inner.err = nil
	if _, err := breaker.Fetch(ctx, target); err != nil {
		t.Fatalf("probe fetch: %v", err)
	}
	if inner.calls != callsAtOpen+1 {
		t.Fatal("the probe should have reached the browser")
	}

	// A successful probe closes the breaker, so the next call flows too.
	_, _ = breaker.Fetch(ctx, target)
	if inner.calls != callsAtOpen+2 {
		t.Fatal("breaker should be closed after a successful probe")
	}
}

func TestBrowserCircuitBreakerHalfOpenReopensOnFailedProbe(t *testing.T) {
	clock := &breakerClock{t: time.Unix(1_000_000, 0)}
	inner := &breakerStubSource{err: errors.New("boom")}
	breaker := newTestBreaker(inner, 2, clock)
	target := breakerTestURL(t)
	ctx := context.Background()

	_, _ = breaker.Fetch(ctx, target)
	_, _ = breaker.Fetch(ctx, target) // opens
	clock.advance(2 * time.Minute)

	callsBeforeProbe := inner.calls
	_, _ = breaker.Fetch(ctx, target) // probe, still failing → reopens
	if inner.calls != callsBeforeProbe+1 {
		t.Fatal("the probe should have reached the browser")
	}

	// The failed probe reopened the breaker, so the next call short-circuits.
	_, _ = breaker.Fetch(ctx, target)
	if inner.calls != callsBeforeProbe+1 {
		t.Fatal("a failed probe should reopen the breaker")
	}
}

// TestBrowserCircuitBreakerAdmitHalfOpenBlocksSecondProbe drives admit's
// half-open arm directly: once the cooldown lapses the first admit promotes the
// open breaker to half-open and lets a single probe through, and a second caller
// arriving while that probe is still in flight is refused.
func TestBrowserCircuitBreakerAdmitHalfOpenBlocksSecondProbe(t *testing.T) {
	clock := &breakerClock{t: time.Unix(1_000_000, 0)}
	breaker := newTestBreaker(&breakerStubSource{}, 2, clock)

	breaker.state = breakerOpen
	breaker.openedAt = clock.t
	clock.advance(2 * time.Minute)

	if !breaker.admit() {
		t.Fatal("first admit after the cooldown should let the probe through")
	}
	if breaker.state != breakerHalfOpen {
		t.Fatalf("state = %d, want half-open after admitting the probe", breaker.state)
	}
	if breaker.admit() {
		t.Fatal("a second admit while half-open must be refused")
	}
}

// TestBrowserCircuitBreakerRecordFailureWhileOpenRefreshesTimer drives record's
// already-open arm (admit normally blocks calls while open): a malfunction
// recorded in that state keeps the breaker open and moves the cooldown timer
// forward to the latest failure.
func TestBrowserCircuitBreakerRecordFailureWhileOpenRefreshesTimer(t *testing.T) {
	clock := &breakerClock{t: time.Unix(1_000_000, 0)}
	breaker := newTestBreaker(&breakerStubSource{}, 2, clock)

	breaker.state = breakerOpen
	breaker.openedAt = clock.t
	clock.advance(30 * time.Second)

	breaker.record(context.Background(), errors.New("boom"))
	if breaker.state != breakerOpen {
		t.Fatalf("state = %d, want the breaker to stay open", breaker.state)
	}
	if !breaker.openedAt.Equal(clock.t) {
		t.Fatalf(
			"openedAt = %v, want refreshed to the latest failure %v",
			breaker.openedAt,
			clock.t,
		)
	}
}

func TestNewBrowserCircuitBreakerDisabledReturnsInner(t *testing.T) {
	inner := &breakerStubSource{}
	if got := NewBrowserCircuitBreaker(inner, 0, time.Minute); got != PageSource(inner) {
		t.Fatal("a non-positive threshold must disable the breaker and return the inner source")
	}
}

// TestNewBrowserCircuitBreakerDefaultsCooldown covers the real constructor (the
// other tests build the breaker directly): a positive threshold returns a live
// breaker, a non-positive cooldown falls back to the default, and the breaker
// opens and short-circuits once the threshold is hit.
func TestNewBrowserCircuitBreakerDefaultsCooldown(t *testing.T) {
	malfunction := errors.New("boom")
	inner := &breakerStubSource{err: malfunction}
	breaker, ok := NewBrowserCircuitBreaker(inner, 1, 0).(*BrowserCircuitBreaker)
	if !ok {
		t.Fatal("a positive threshold must return a *BrowserCircuitBreaker")
	}
	if breaker.cooldown != DefaultBrowserBreakerCooldown {
		t.Fatalf(
			"cooldown = %v, want the default %v",
			breaker.cooldown,
			DefaultBrowserBreakerCooldown,
		)
	}

	ctx := context.Background()
	target := breakerTestURL(t)
	if _, err := breaker.Fetch(ctx, target); !errors.Is(err, malfunction) {
		t.Fatalf("first fetch error = %v, want the browser malfunction", err)
	}
	if _, err := breaker.Fetch(ctx, target); !errors.Is(err, ErrPageRejected) {
		t.Fatalf("open-circuit fetch error = %v, want a page rejection", err)
	}
	if inner.calls != 1 {
		t.Fatalf("browser called %d times, want 1 (an open circuit stops calling it)", inner.calls)
	}
}
