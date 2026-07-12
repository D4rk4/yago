package faviconproxy

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestIconCacheHonorsExactRetainedBudget(t *testing.T) {
	host := "exact.example"
	contentType := "image/png"
	body := []byte("exact-body")
	retained := retainedIconHostBytes(host, contentType) + retainedIconBodyBytes(len(body))
	exact := newIconCache(retained, 1)
	exact.put(host, body, contentType, clock().Add(time.Hour))
	if exact.totalBytes != retained {
		t.Fatalf("exact retained bytes = %d, want %d", exact.totalBytes, retained)
	}
	if got, _, ok := exact.get(host); !ok || !bytes.Equal(got, body) {
		t.Fatal("exact-budget entry was not cached")
	}
	under := newIconCache(retained-1, 1)
	under.put(host, body, contentType, clock().Add(time.Hour))
	if under.totalBytes != 0 || len(under.hosts) != 0 || len(under.bodies) != 0 {
		t.Fatalf("oversized entry retained = %d", under.totalBytes)
	}
}

func TestIconCacheBoundsNegativeKeysByBytes(t *testing.T) {
	contentType := "image/png"
	budget := retainedIconHostBytes("a.example", contentType)
	cache := newIconCache(budget, maxCacheEntries)
	expires := clock().Add(time.Hour)
	cache.put("a.example", nil, contentType, expires)
	cache.put("b.example", nil, contentType, expires)
	if cache.totalBytes != budget || len(cache.hosts) != 1 {
		t.Fatalf("negative cache = %d bytes, %d entries", cache.totalBytes, len(cache.hosts))
	}
	if _, _, ok := cache.get("a.example"); ok {
		t.Fatal("old negative key survived byte eviction")
	}
	if body, gotType, ok := cache.get("b.example"); !ok || body != nil || gotType != contentType {
		t.Fatalf("new negative entry = %v %q %v", body, gotType, ok)
	}
}

func TestIconCacheSharedBodyNearBudgetDoesNotDoubleCharge(t *testing.T) {
	body := []byte("shared")
	contentType := "image/png"
	entryBytes := retainedIconHostBytes("a.example", contentType)
	budget := retainedIconBodyBytes(len(body)) + 2*entryBytes
	cache := newIconCache(budget, maxCacheEntries)
	expires := clock().Add(time.Hour)
	cache.put("a.example", body, contentType, expires)
	cache.put("c.example", nil, contentType, expires)
	cache.get("a.example")
	cache.put("b.example", body, contentType, expires)
	if _, _, ok := cache.get("b.example"); !ok {
		t.Fatal("shared-body alias was rejected near the byte budget")
	}
	if _, _, ok := cache.get("c.example"); ok {
		t.Fatal("least-recent negative entry survived shared-body admission")
	}
	if len(cache.bodies) != 1 || cache.bodies[bodyDigest(body)].refs != 2 ||
		cache.totalBytes != budget {
		t.Fatalf("shared cache retained=%d bodies=%d", cache.totalBytes, len(cache.bodies))
	}
}

func TestIconCacheOversizedReplacementPreservesExisting(t *testing.T) {
	host := "replace.example"
	body := []byte("body")
	contentType := "image/png"
	budget := retainedIconHostBytes(host, contentType) + retainedIconBodyBytes(len(body))
	cache := newIconCache(budget, 1)
	cache.put(host, body, contentType, clock().Add(time.Hour))
	cache.put(host, body, contentType+"x", clock().Add(time.Hour))
	gotBody, gotType, ok := cache.get(host)
	if !ok || !bytes.Equal(gotBody, body) || gotType != contentType || cache.totalBytes != budget {
		t.Fatalf("replacement = %q %q %v retained=%d", gotBody, gotType, ok, cache.totalBytes)
	}
}

func TestIconCacheOwnsBodiesAndReleasesSharedMetadata(t *testing.T) {
	body := []byte("shared")
	cache := newIconCache(maxCacheBytes, maxCacheEntries)
	expires := clock().Add(time.Hour)
	cache.put("a.example", body, "image/png", expires)
	cache.put("b.example", body, "image/png", expires)
	body[0] = 'X'
	cache.put("a.example", nil, "image/png", expires)
	got, _, ok := cache.get("b.example")
	if !ok || string(got) != "shared" || len(cache.bodies) != 1 ||
		cache.bodies[bodyDigest(got)].refs != 1 {
		t.Fatalf("shared body = %q found=%v bodies=%d", got, ok, len(cache.bodies))
	}
	cache.put("b.example", nil, "image/png", expires)
	wantBytes := retainedIconHostBytes("a.example", "image/png") +
		retainedIconHostBytes("b.example", "image/png")
	if len(cache.bodies) != 0 || cache.totalBytes != wantBytes {
		t.Fatalf("released shared body = %d bodies, %d bytes", len(cache.bodies), cache.totalBytes)
	}
}

func TestIconCacheConcurrentRetentionBound(t *testing.T) {
	cache := newIconCache(32<<10, 32)
	expires := clock().Add(time.Hour)
	var requests sync.WaitGroup
	for index := 0; index < 256; index++ {
		requests.Add(1)
		go func(index int) {
			defer requests.Done()
			host := fmt.Sprintf("host-%03d.example", index)
			body := []byte(fmt.Sprintf("body-%03d", index%17))
			cache.put(host, body, "image/png", expires)
			cache.get(host)
		}(index)
	}
	requests.Wait()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.totalBytes > cache.maxBytes || len(cache.hosts) > cache.maxEntries ||
		cache.order.Len() != len(cache.hosts) {
		t.Fatalf(
			"cache retained=%d hosts=%d order=%d",
			cache.totalBytes,
			len(cache.hosts),
			cache.order.Len(),
		)
	}
	refs := 0
	for _, body := range cache.bodies {
		refs += body.refs
	}
	if refs != len(cache.hosts) {
		t.Fatalf("body refs = %d, hosts = %d", refs, len(cache.hosts))
	}
}
