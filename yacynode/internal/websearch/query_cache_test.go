package websearch

import (
	"testing"
	"time"
)

func TestQueryCachePutGet(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	cache := newQueryCache(time.Minute, 8, func() time.Time { return clock })
	cache.put("q", []Result{{Title: "one"}})

	got, ok := cache.get("q")
	if !ok || len(got) != 1 || got[0].Title != "one" {
		t.Fatalf("get = %#v, %v", got, ok)
	}
}

func TestQueryCacheExpires(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	cache := newQueryCache(time.Minute, 8, func() time.Time { return clock })
	cache.put("q", []Result{{Title: "one"}})

	clock = clock.Add(2 * time.Minute)
	if _, ok := cache.get("q"); ok {
		t.Fatal("entry should have expired")
	}
}

func TestQueryCacheDisabledWhenTTLZero(t *testing.T) {
	cache := newQueryCache(0, 8, func() time.Time { return time.Unix(0, 0) })
	cache.put("q", []Result{{Title: "one"}})
	if _, ok := cache.get("q"); ok {
		t.Fatal("cache with zero TTL must not retain entries")
	}
}

func TestQueryCacheBounded(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	cache := newQueryCache(time.Hour, 3, func() time.Time { return clock })
	for _, key := range []string{"a", "b", "c", "d", "e"} {
		cache.put(key, []Result{{Title: key}})
	}
	if len(cache.entries) > 3 {
		t.Fatalf("cache size = %d, want <= 3", len(cache.entries))
	}
}
