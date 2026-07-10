package pagefetch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"
)

const (
	// DefaultBrowserFailureThreshold opens the breaker after this many
	// consecutive browser malfunctions.
	DefaultBrowserFailureThreshold = 5
	// DefaultBrowserBreakerCooldown is how long the breaker stays open before it
	// admits a single probe fetch to test whether the browser recovered.
	DefaultBrowserBreakerCooldown = 2 * time.Minute
)

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// errBrowserCircuitOpen is the rejection returned while the breaker is open. It
// wraps ErrPageRejected so it reads as a page skip (never a fatal fetch error)
// and never triggers further escalation.
var errBrowserCircuitOpen = fmt.Errorf(
	"browser fallback paused after repeated failures: %w", ErrPageRejected,
)

// BrowserCircuitBreaker wraps the slow-path browser source and stops calling it
// after a run of consecutive malfunctions — a launch failure, a dead browser
// process, a navigation timeout: anything that is not a clean content rejection.
// While open it returns a page rejection immediately, so a crashed or
// incompatible browser does not relaunch and spam identical failures on every
// fetch (the failure mode that motivated it on a host where the browser could
// not start at all). After a cooldown it admits one probe fetch and closes again
// on the first success, so a transient outage self-heals. A clean content
// rejection (the browser refusing a non-HTML media type) proves the browser is
// alive and resets the breaker.
type BrowserCircuitBreaker struct {
	inner     PageSource
	threshold int
	cooldown  time.Duration
	now       func() time.Time

	mu       sync.Mutex
	state    breakerState
	failures int
	openedAt time.Time
}

// NewBrowserCircuitBreaker wraps inner with a breaker. A threshold of zero or
// less disables the breaker and returns inner unchanged, so an operator can opt
// out; a non-positive cooldown falls back to the default.
func NewBrowserCircuitBreaker(
	inner PageSource,
	threshold int,
	cooldown time.Duration,
) PageSource {
	if threshold <= 0 {
		return inner
	}
	if cooldown <= 0 {
		cooldown = DefaultBrowserBreakerCooldown
	}

	return &BrowserCircuitBreaker{
		inner:     inner,
		threshold: threshold,
		cooldown:  cooldown,
		now:       time.Now,
	}
}

func (b *BrowserCircuitBreaker) Fetch(
	ctx context.Context,
	target *url.URL,
) (FetchedPage, error) {
	if !b.admit() {
		return FetchedPage{}, errBrowserCircuitOpen
	}
	page, err := b.inner.Fetch(ctx, target)
	b.record(ctx, err)
	if err != nil {
		return FetchedPage{}, fmt.Errorf("browser fetch: %w", err)
	}

	return page, nil
}

// admit reports whether this fetch may reach the browser. An open breaker
// transitions to half-open once the cooldown elapses and lets a single probe
// through; every other caller is held back while a probe is in flight or the
// cooldown has not expired.
func (b *BrowserCircuitBreaker) admit() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.state {
	case breakerOpen:
		if b.now().Sub(b.openedAt) < b.cooldown {
			return false
		}
		b.state = breakerHalfOpen

		return true
	case breakerHalfOpen:
		return false
	default: // breakerClosed
		return true
	}
}

// record folds one fetch outcome into the breaker state. A success or a clean
// content rejection proves the browser is alive and closes the breaker; any
// other error is a malfunction that counts toward opening.
func (b *BrowserCircuitBreaker) record(ctx context.Context, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if err == nil || errors.Is(err, ErrPageRejected) {
		if b.state != breakerClosed {
			slog.InfoContext(ctx, "browser fallback recovered; circuit closed")
		}
		b.state = breakerClosed
		b.failures = 0

		return
	}

	b.failures++
	switch b.state {
	case breakerHalfOpen:
		// The probe failed; reopen with a fresh cooldown.
		b.state = breakerOpen
		b.openedAt = b.now()
	case breakerOpen:
		// Should not happen (admit blocks calls while open), but keep the timer
		// fresh so the cooldown measures from the latest failure.
		b.openedAt = b.now()
	default: // breakerClosed
		if b.failures >= b.threshold {
			b.state = breakerOpen
			b.openedAt = b.now()
			slog.WarnContext(ctx, "browser fallback circuit opened after consecutive failures",
				slog.Int("failures", b.failures))
		}
	}
}
