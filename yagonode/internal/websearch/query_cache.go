package websearch

import (
	"sync"
	"time"
)

type cacheEntry struct {
	results   []Result
	expiresAt time.Time
}

// queryCache is a small bounded, TTL-expiring cache of provider responses keyed
// by query. It exists to respect the external backend's rate limits and to avoid
// repeat egress for the same miss within a short window.
type queryCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	ttl     time.Duration
	max     int
	now     func() time.Time
}

func newQueryCache(ttl time.Duration, maxEntries int, now func() time.Time) *queryCache {
	return &queryCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
		max:     maxEntries,
		now:     now,
	}
}

func (c *queryCache) get(key string) ([]Result, bool) {
	if c.ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if !c.now().Before(entry.expiresAt) {
		delete(c.entries, key)

		return nil, false
	}

	return entry.results, true
}

func (c *queryCache) put(key string, results []Result) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max && c.max > 0 {
		c.evictExpiredLocked()
	}
	if len(c.entries) >= c.max && c.max > 0 {
		c.evictOneLocked()
	}
	c.entries[key] = cacheEntry{results: results, expiresAt: c.now().Add(c.ttl)}
}

func (c *queryCache) evictExpiredLocked() {
	now := c.now()
	for key, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}

func (c *queryCache) evictOneLocked() {
	for key := range c.entries {
		delete(c.entries, key)

		return
	}
}
