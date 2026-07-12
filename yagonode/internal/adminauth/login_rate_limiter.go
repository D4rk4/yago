package adminauth

import (
	"strings"
	"sync"
	"time"
)

const (
	maximumTrackedLoginClients    = 4096
	maximumLoginFailuresPerClient = 64
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
		max:      min(max(1, maxFailures), maximumLoginFailuresPerClient),
		window:   window,
		now:      now,
	}
}

func (l *loginRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.purgeStaleLocked(now)
	recent, found := l.failures[key]
	if !found && len(l.failures) >= maximumTrackedLoginClients {
		return false
	}

	return len(recent) < l.max
}

func (l *loginRateLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.purgeStaleLocked(now)
	recent, found := l.failures[key]
	if !found {
		if len(l.failures) >= maximumTrackedLoginClients {
			return
		}
		key = strings.Clone(key)
		recent = make([]time.Time, 0, l.max)
	}
	if len(recent) >= l.max {
		return
	}

	l.failures[key] = append(recent, now)
}

func (l *loginRateLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.failures, key)
}

func (l *loginRateLimiter) purgeStaleLocked(now time.Time) {
	for key := range l.failures {
		l.recentLocked(key, now)
	}
}

func (l *loginRateLimiter) recentLocked(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
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
