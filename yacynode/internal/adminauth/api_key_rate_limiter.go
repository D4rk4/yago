package adminauth

import (
	"sync"
	"time"
)

// apiKeyRateLimiter caps how many requests a single API key may make within a
// sliding window. Unlike the login limiter it records every allowed request,
// not only failures, so it bounds total request volume per key.
type apiKeyRateLimiter struct {
	mu     sync.Mutex
	events map[string][]time.Time
	max    int
	window time.Duration
	now    func() time.Time
}

func newAPIKeyRateLimiter(
	maxPerWindow int,
	window time.Duration,
	now func() time.Time,
) *apiKeyRateLimiter {
	return &apiKeyRateLimiter{
		events: map[string][]time.Time{},
		max:    maxPerWindow,
		window: window,
		now:    now,
	}
}

func (l *apiKeyRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	recent := l.recentLocked(key)
	if len(recent) >= l.max {
		return false
	}
	l.events[key] = append(recent, l.now())

	return true
}

func (l *apiKeyRateLimiter) recentLocked(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	kept := l.events[key][:0]
	for _, at := range l.events[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(l.events, key)

		return nil
	}
	l.events[key] = kept

	return kept
}
