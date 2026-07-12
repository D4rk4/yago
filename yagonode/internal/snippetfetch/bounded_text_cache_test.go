package snippetfetch

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestBoundedTextCacheExpiresAndReplaces(t *testing.T) {
	current := time.Unix(1, 0)
	cache := newBoundedTextCache(func() time.Time { return current })
	if _, ok := cache.get("missing"); ok {
		t.Fatal("missing cache entry was found")
	}
	cache.put("url", "old")
	cache.put("url", "new text")
	if text, ok := cache.get("url"); !ok || text != "new text" {
		t.Fatalf("cache entry = %q, %v", text, ok)
	}
	if cache.bytes != len("url")+len("new text") {
		t.Fatalf("cache bytes = %d", cache.bytes)
	}
	current = current.Add(cacheTTL)
	if _, ok := cache.get("url"); ok || cache.bytes != 0 || len(cache.entries) != 0 {
		t.Fatalf("expired cache = %#v bytes=%d", cache.entries, cache.bytes)
	}
}

func TestBoundedTextCacheCapsEntriesAndBytes(t *testing.T) {
	cache := newBoundedTextCache(time.Now)
	for index := range cacheMaximumEntries + 1 {
		cache.put(fmt.Sprintf("url-%d", index), "text")
	}
	if len(cache.entries) > cacheMaximumEntries {
		t.Fatalf("cache entries = %d", len(cache.entries))
	}

	cache = newBoundedTextCache(time.Now)
	chunk := strings.Repeat("x", 1<<20)
	for index := range 9 {
		cache.put(fmt.Sprintf("url-%d", index), chunk)
	}
	if cache.bytes > cacheMaximumBytes {
		t.Fatalf("cache bytes = %d", cache.bytes)
	}

	cache = newBoundedTextCache(time.Now)
	cache.put("oversized", strings.Repeat("x", cacheMaximumBytes))
	if len(cache.entries) != 0 || cache.bytes != 0 {
		t.Fatalf("oversized entry was cached: bytes=%d", cache.bytes)
	}
}
