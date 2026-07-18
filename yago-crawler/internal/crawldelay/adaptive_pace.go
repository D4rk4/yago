package crawldelay

import (
	"fmt"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
)

const (
	// maxHostBackoff caps how long a throttled host is set aside, Heritrix's
	// retryDelaySeconds default (900s); Retry-After wishes are clamped to it so
	// a hostile header cannot park a host for days.
	maxHostBackoff = 15 * time.Minute
	// parkAfterFailures is how many consecutive throttles or hard fetch errors
	// park a host at the full backoff cap, Nutch's per-host exception purge
	// threshold.
	parkAfterFailures = 5
)

// BackoffObserver is notified each time a host is backed off, so an edge can
// meter server-signalled restraint. It must not block.
type BackoffObserver interface {
	ObserveHostBackoff()
}

type hostBackoff struct {
	until      time.Time
	penalty    time.Duration
	failures   uint32
	generation uint64
}

// AdaptivePace layers server-load-aware restraint over the fixed per-host
// crawl delay: a 429/503 (or repeated hard failure) multiplies the host's
// penalty (honoring Retry-After when the server names a wish), consecutive
// failures park the host, and every success halves the penalty back toward
// the base delay — AIMD-style recovery, so one rough patch does not exile a
// host forever (Scrapy AutoThrottle and Nutch exponential backoff are the
// production precedents).
type AdaptivePace struct {
	inner    *HostPace
	observer BackoffObserver

	mu      sync.Mutex
	backoff *lru.Cache[string, hostBackoff]
}

// NewAdaptivePace wraps the fixed host pace with adaptive backoff state for up
// to hostCacheSize hosts. A nil observer is allowed and stays silent.
func NewAdaptivePace(
	inner *HostPace,
	hostCacheSize int,
	observer BackoffObserver,
) (*AdaptivePace, error) {
	backoff, err := lru.New[string, hostBackoff](hostCacheSize)
	if err != nil {
		return nil, fmt.Errorf("adaptive pace host cache: %w", err)
	}

	return &AdaptivePace{inner: inner, observer: observer, backoff: backoff}, nil
}

// DueAt is the later of the fixed-pace due time and the host's backoff window.
func (p *AdaptivePace) DueAt(job crawljob.CrawlJob, now time.Time) time.Time {
	due := p.inner.DueAt(job, now)
	p.mu.Lock()
	defer p.mu.Unlock()
	if state, ok := p.backoff.Get(weburl.Host(job.URL)); ok && state.until.After(due) {
		return state.until
	}

	return due
}

// Visited delegates to the fixed pace; adaptive state changes only on explicit
// throttle and success feedback.
func (p *AdaptivePace) Visited(job crawljob.CrawlJob, at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inner.Visited(job, at)
}

// Throttled backs the URL's host off after a server-load signal: the penalty
// doubles per consecutive signal starting from twice the base delay, honors a
// Retry-After wish when it asks for more, is clamped to maxHostBackoff, and
// after parkAfterFailures consecutive signals the host sits at the full cap.
func (p *AdaptivePace) Throttled(rawURL string, retryAfter time.Duration, now time.Time) {
	host := weburl.Host(rawURL)
	p.mu.Lock()
	defer p.mu.Unlock()
	state, _ := p.backoff.Get(host)
	penalty := max(2*p.inner.delay, 2*state.penalty, retryAfter)
	state.failures++
	if state.failures >= parkAfterFailures || penalty > maxHostBackoff {
		penalty = maxHostBackoff
	}
	state.penalty = penalty
	state.until = now.Add(penalty)
	state.generation = p.inner.nextGeneration()
	p.backoff.Add(host, state)
	if p.observer != nil {
		p.observer.ObserveHostBackoff()
	}
}

// Succeeded eases the URL's host back toward the base delay: the penalty
// halves per success (slow-start recovery) and the consecutive-failure count
// resets; a penalty back under the base delay clears the host's state.
func (p *AdaptivePace) Succeeded(rawURL string, now time.Time) {
	host := weburl.Host(rawURL)
	p.mu.Lock()
	defer p.mu.Unlock()
	state, ok := p.backoff.Get(host)
	if !ok {
		return
	}
	state.failures = 0
	state.penalty /= 2
	state.generation = p.inner.nextGeneration()
	if state.penalty <= p.inner.delay {
		state.until = time.Time{}
		state.penalty = 0
		p.backoff.Add(host, state)

		return
	}
	state.until = now.Add(state.penalty)
	p.backoff.Add(host, state)
}

func (p *AdaptivePace) SnapshotHost(rawURL string) crawlpace.HostState {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.inner.SnapshotHost(rawURL)
	backoff, found := p.backoff.Get(weburl.Host(rawURL))
	if !found {
		return state
	}
	state.BackoffUntil = backoff.until
	state.BackoffPenalty = backoff.penalty
	state.BackoffFailures = backoff.failures
	state.Generation = max(state.Generation, backoff.generation)

	return state
}

func (p *AdaptivePace) RestoreHost(host string, state crawlpace.HostState) {
	if state.Generation == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inner.RestoreHost(host, state)
	current, found := p.backoff.Get(host)
	if found && current.generation > state.Generation {
		return
	}
	p.backoff.Add(host, hostBackoff{
		until:      state.BackoffUntil,
		penalty:    state.BackoffPenalty,
		failures:   state.BackoffFailures,
		generation: state.Generation,
	})
}

func (p *AdaptivePace) Capacity() int {
	return p.inner.Capacity()
}
