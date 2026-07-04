package adminauth

import (
	"sync"
	"time"
)

type loginRateLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
	max      int
	window   time.Duration
	now      func() time.Time
}

func newLoginRateLimiter(
	maxFailures int,
	window time.Duration,
	now func() time.Time,
) *loginRateLimiter {
	return &loginRateLimiter{
		failures: map[string][]time.Time{},
		max:      maxFailures,
		window:   window,
		now:      now,
	}
}

func (l *loginRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.recentLocked(key)) < l.max
}

func (l *loginRateLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.failures[key] = append(l.recentLocked(key), l.now())
}

func (l *loginRateLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.failures, key)
}

func (l *loginRateLimiter) recentLocked(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	kept := l.failures[key][:0]
	for _, at := range l.failures[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)

		return nil
	}
	l.failures[key] = kept

	return kept
}
