package websearch

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestQueryCachePutGet(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	cache := newQueryCache(time.Minute, 8, defaultCacheBytes, func() time.Time { return clock })
	results := []Result{{Title: "one"}}
	cache.put("q", results)
	results[0].Title = "changed"

	got, ok := cache.get("q")
	if !ok || len(got) != 1 || got[0].Title != "one" {
		t.Fatalf("get = %#v, %v", got, ok)
	}
	got[0].Title = "caller changed"
	got, ok = cache.get("q")
	if !ok || got[0].Title != "one" {
		t.Fatalf("second get = %#v, %v", got, ok)
	}
}

func TestQueryCacheExpires(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	cache := newQueryCache(time.Minute, 8, defaultCacheBytes, func() time.Time { return clock })
	cache.put("q", []Result{{Title: "one"}})

	clock = clock.Add(2 * time.Minute)
	if _, ok := cache.get("q"); ok {
		t.Fatal("entry should have expired")
	}
	if cache.bytes != 0 {
		t.Fatalf("retained bytes = %d, want 0", cache.bytes)
	}
}

func TestQueryCacheDisabledWhenByteBudgetZero(t *testing.T) {
	cache := newQueryCache(time.Minute, 8, 0, func() time.Time { return time.Unix(0, 0) })
	cache.put("q", []Result{{Title: "one"}})
	if _, ok := cache.get("q"); ok {
		t.Fatal("cache with zero byte budget must not retain entries")
	}
}

func TestQueryCacheDisabledWhenTTLZero(t *testing.T) {
	cache := newQueryCache(0, 8, defaultCacheBytes, func() time.Time { return time.Unix(0, 0) })
	cache.put("q", []Result{{Title: "one"}})
	if _, ok := cache.get("q"); ok {
		t.Fatal("cache with zero TTL must not retain entries")
	}
}

func TestQueryCacheBounded(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	cache := newQueryCache(time.Hour, 3, defaultCacheBytes, func() time.Time { return clock })
	for _, key := range []string{"a", "b", "c", "d", "e"} {
		cache.put(key, []Result{{Title: key}})
	}
	if len(cache.entries) > 3 {
		t.Fatalf("cache size = %d, want <= 3", len(cache.entries))
	}
	if cache.bytes != 6 {
		t.Fatalf("retained bytes = %d, want 6", cache.bytes)
	}
}

func TestQueryCacheBoundedByBytes(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	cache := newQueryCache(time.Hour, 8, 14, func() time.Time { return clock })
	cache.put("a", []Result{{Title: "123", URL: "456", Snippet: "789"}})
	if cache.bytes != 10 {
		t.Fatalf("retained bytes = %d, want 10", cache.bytes)
	}
	cache.put("bb", []Result{{Title: "1234", URL: "5678", Snippet: "9012"}})
	if cache.bytes != 14 || len(cache.entries) != 1 {
		t.Fatalf("cache bytes/entries = %d/%d, want 14/1", cache.bytes, len(cache.entries))
	}
	if _, ok := cache.get("a"); ok {
		t.Fatal("older entry should be evicted to satisfy byte budget")
	}
}

func TestQueryCacheRejectsOversizedReplacement(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	cache := newQueryCache(time.Hour, 8, 8, func() time.Time { return clock })
	cache.put("q", []Result{{Title: "one"}})
	cache.put("q", []Result{{Title: "12345678"}})
	if len(cache.entries) != 0 || cache.bytes != 0 {
		t.Fatalf("cache bytes/entries = %d/%d, want 0/0", cache.bytes, len(cache.entries))
	}
}

func TestQueryCacheConcurrentByteBound(t *testing.T) {
	cache := newQueryCache(time.Hour, 8, 256, time.Now)
	var workers sync.WaitGroup
	for worker := range 32 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			key := fmt.Sprintf("query-%d", worker)
			cache.put(key, []Result{{
				Title: strings.Repeat("t", 16), URL: "https://example.test", Snippet: key,
			}})
			_, _ = cache.get(key)
		}()
	}
	workers.Wait()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) > cache.max || cache.bytes > cache.maxBytes {
		t.Fatalf(
			"cache bytes/entries = %d/%d, limits %d/%d",
			cache.bytes,
			len(cache.entries),
			cache.maxBytes,
			cache.max,
		)
	}
}
