package snippetfetch

import (
	"sync"
	"time"
)

const (
	cacheMaximumEntries = 128
	cacheMaximumBytes   = 8 << 20
	cacheTTL            = time.Hour
)

type cachedText struct {
	text      string
	fetchedAt time.Time
	size      int
}

type boundedTextCache struct {
	mu      sync.Mutex
	entries map[string]cachedText
	bytes   int
	now     func() time.Time
}

func newBoundedTextCache(now func() time.Time) *boundedTextCache {
	return &boundedTextCache{entries: map[string]cachedText{}, now: now}
}

func (c *boundedTextCache) get(rawURL string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[rawURL]
	if !ok {
		return "", false
	}
	if c.now().Sub(entry.fetchedAt) >= cacheTTL {
		delete(c.entries, rawURL)
		c.bytes -= entry.size

		return "", false
	}

	return entry.text, true
}

func (c *boundedTextCache) put(rawURL string, text string) {
	size := len(rawURL) + len(text)
	if size > cacheMaximumBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.entries[rawURL]; ok {
		delete(c.entries, rawURL)
		c.bytes -= existing.size
	}
	if len(c.entries) >= cacheMaximumEntries || c.bytes+size > cacheMaximumBytes {
		c.entries = map[string]cachedText{}
		c.bytes = 0
	}
	c.entries[rawURL] = cachedText{text: text, fetchedAt: c.now(), size: size}
	c.bytes += size
}
