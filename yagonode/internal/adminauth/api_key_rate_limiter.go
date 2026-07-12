package adminauth

import (
	"container/list"
	"strings"
	"sync"
	"time"
)

const (
	maximumTrackedAPIKeys = 4096
	maximumAPIKeyEvents   = 256
)

type apiKeyEvents struct {
	stamps   []time.Time
	keyOrder *list.Element
}

// apiKeyRateLimiter caps how many requests a single API key may make within a
// sliding window. Unlike the login limiter it records every allowed request,
// not only failures, so it bounds total request volume per key.
type apiKeyRateLimiter struct {
	mu     sync.Mutex
	events map[string]*apiKeyEvents
	order  list.List
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
		events: map[string]*apiKeyEvents{},
		max:    min(max(1, maxPerWindow), maximumAPIKeyEvents),
		window: window,
		now:    now,
	}
}

func (l *apiKeyRateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.purgeStaleLocked(now)
	entry := l.events[key]
	if entry == nil {
		if len(l.events) >= maximumTrackedAPIKeys {
			return false
		}
		key = strings.Clone(key)
		entry = &apiKeyEvents{stamps: make([]time.Time, 0, l.max)}
		entry.keyOrder = l.order.PushBack(key)
		l.events[key] = entry
	} else {
		entry.stamps = recentAPIKeyEvents(entry.stamps, now.Add(-l.window))
	}

	if len(entry.stamps) >= l.max {
		return false
	}
	entry.stamps = append(entry.stamps, now)
	l.order.MoveToBack(entry.keyOrder)

	return true
}

func (l *apiKeyRateLimiter) purgeStaleLocked(now time.Time) {
	cutoff := now.Add(-l.window)
	for oldest := l.order.Front(); oldest != nil; oldest = l.order.Front() {
		key := oldest.Value.(string)
		entry := l.events[key]
		if entry.stamps[len(entry.stamps)-1].After(cutoff) {
			return
		}
		delete(l.events, key)
		l.order.Remove(oldest)
	}
}

func recentAPIKeyEvents(events []time.Time, cutoff time.Time) []time.Time {
	kept := events[:0]
	for _, at := range events {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}

	return kept
}
