package websearch

import (
	"strings"
	"sync"
	"time"
)

type cacheEntry struct {
	results   []Result
	expiresAt time.Time
	bytes     int
}

// queryCache is a small bounded, TTL-expiring cache of provider responses keyed
// by query. It exists to respect the external backend's rate limits and to avoid
// repeat egress for the same miss within a short window.
type queryCache struct {
	mu       sync.Mutex
	entries  map[string]cacheEntry
	ttl      time.Duration
	max      int
	maxBytes int
	bytes    int
	now      func() time.Time
}

func newQueryCache(
	ttl time.Duration,
	maxEntries int,
	maxBytes int,
	now func() time.Time,
) *queryCache {
	return &queryCache{
		entries:  make(map[string]cacheEntry),
		ttl:      ttl,
		max:      maxEntries,
		maxBytes: maxBytes,
		now:      now,
	}
}

func (c *queryCache) get(key string) ([]Result, bool) {
	if c.ttl <= 0 || c.maxBytes <= 0 {
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
		c.bytes -= entry.bytes

		return nil, false
	}

	return cloneResults(entry.results), true
}

func (c *queryCache) put(key string, results []Result) {
	if c.ttl <= 0 || c.maxBytes <= 0 {
		return
	}
	entryBytes := cachedResultBytes(key, results)
	c.mu.Lock()
	defer c.mu.Unlock()
	if previous, ok := c.entries[key]; ok {
		delete(c.entries, key)
		c.bytes -= previous.bytes
	}
	if entryBytes > c.maxBytes {
		return
	}
	c.evictExpiredLocked()
	for (len(c.entries) >= c.max && c.max > 0) || c.bytes > c.maxBytes-entryBytes {
		c.evictOneLocked()
	}
	c.entries[strings.Clone(key)] = cacheEntry{
		results:   cloneResults(results),
		expiresAt: c.now().Add(c.ttl),
		bytes:     entryBytes,
	}
	c.bytes += entryBytes
}

func (c *queryCache) evictExpiredLocked() {
	now := c.now()
	for key, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, key)
			c.bytes -= entry.bytes
		}
	}
}

func (c *queryCache) evictOneLocked() {
	for key, entry := range c.entries {
		delete(c.entries, key)
		c.bytes -= entry.bytes

		return
	}
}

func cachedResultBytes(key string, results []Result) int {
	bytes := len(key)
	for _, result := range results {
		bytes += len(result.Title) + len(result.URL) + len(result.Snippet)
	}

	return bytes
}

func cloneResults(results []Result) []Result {
	cloned := make([]Result, len(results))
	for index, result := range results {
		cloned[index] = Result{
			Title:   strings.Clone(result.Title),
			URL:     strings.Clone(result.URL),
			Snippet: strings.Clone(result.Snippet),
		}
	}

	return cloned
}
