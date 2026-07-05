package faviconproxy

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	return buf.Bytes()
}

// originAndProxy serves the given favicon response over TLS and returns a proxy
// whose client trusts the origin and rewrites every dial to it.
func originAndProxy(t *testing.T, handler http.HandlerFunc) (*Proxy, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(origin.Close)

	client := origin.Client()
	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatalf("parse origin url: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("origin client transport is not *http.Transport")
	}
	transport.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
	// Rewrite every request to the local TLS origin regardless of the host in
	// the URL, so tests exercise the real fetch path hermetically.
	client.Transport = rewriteTransport{next: transport, target: originURL.Host}

	return New(client, 2), &hits
}

type rewriteTransport struct {
	next   http.RoundTripper
	target string
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Host = t.target

	//nolint:wrapcheck // transparent test transport.
	return t.next.RoundTrip(req)
}

func get(t *testing.T, proxy *Proxy, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET "+Path, proxy)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))

	return rec
}

func TestProxyServesFetchedIconAndCaches(t *testing.T) {
	icon := pngBytes(t)
	proxy, hits := originAndProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/favicon.ico" {
			http.NotFound(w, r)

			return
		}
		_, _ = w.Write(icon) // nosemgrep
	})

	rec := get(t, proxy, URLFor("example.org"))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), icon) {
		t.Fatalf("status=%d body-len=%d, want fetched icon", rec.Code, rec.Body.Len())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q", got)
	}
	if rec.Header().Get("Cache-Control") == "" || rec.Header().Get("X-Content-Type-Options") == "" {
		t.Fatal("cache/nosniff headers missing")
	}

	if rec := get(t, proxy, URLFor("example.org")); !bytes.Equal(rec.Body.Bytes(), icon) {
		t.Fatal("cached response differs")
	}
	if hits.Load() != 1 {
		t.Fatalf("origin fetched %d times, want 1 (cache)", hits.Load())
	}
}

func TestProxyPlaceholderPaths(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"missing": func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
		"not an image": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>nope</html>"))
		},
		"svg rejected": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`))
		},
		"oversize": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(append(pngBytes(t), make([]byte, maxIconBytes)...)) // nosemgrep
		},
		"empty": func(http.ResponseWriter, *http.Request) {},
	}
	for name, handler := range cases {
		proxy, _ := originAndProxy(t, handler)
		rec := get(t, proxy, URLFor("example.org"))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), proxy.placeholder) {
			t.Fatalf("%s: status=%d, want the placeholder", name, rec.Code)
		}
	}
}

func TestProxyRejectsInvalidHosts(t *testing.T) {
	proxy := New(&http.Client{}, 1)
	for _, target := range []string{
		Path,
		URLFor("host:8443"),
		URLFor("host/path"),
		URLFor("user@host"),
		URLFor("[::1]"),
		URLFor("two hosts"),
		Path + "?host=%25zz", // unparseable once prefixed with the scheme
		Path + "?host=a%23b", // fragment marker splits the hostname
	} {
		if rec := get(t, proxy, target); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", target, rec.Code)
		}
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrHandlerTimeout
}

func TestProxyPlaceholderOnTransportError(t *testing.T) {
	proxy := New(&http.Client{Transport: failingTransport{}}, 1)
	rec := get(t, proxy, URLFor("example.org"))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), proxy.placeholder) {
		t.Fatal("transport failure must answer with the placeholder")
	}
}

func TestProxyPlaceholderWhenFetchSlotsBusy(t *testing.T) {
	proxy := New(&http.Client{}, 1)
	proxy.slots <- struct{}{}

	rec := get(t, proxy, URLFor("example.org"))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), proxy.placeholder) {
		t.Fatal("busy fetch slots must answer with the placeholder")
	}
}

func TestProxyNegativeCacheExpires(t *testing.T) {
	base := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	current := base
	oldClock := clock
	t.Cleanup(func() { clock = oldClock })
	clock = func() time.Time { return current }

	icon := pngBytes(t)
	fail := atomic.Bool{}
	fail.Store(true)
	proxy, hits := originAndProxy(t, func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.NotFound(w, r)

			return
		}
		_, _ = w.Write(icon) // nosemgrep
	})

	if rec := get(
		t,
		proxy,
		URLFor("example.org"),
	); !bytes.Equal(
		rec.Body.Bytes(),
		proxy.placeholder,
	) {
		t.Fatal("first fetch should fail to the placeholder")
	}
	if rec := get(
		t,
		proxy,
		URLFor("example.org"),
	); !bytes.Equal(
		rec.Body.Bytes(),
		proxy.placeholder,
	) {
		t.Fatal("negative cache should still serve the placeholder")
	}
	if hits.Load() != 1 {
		t.Fatalf("origin fetched %d times during negative TTL, want 1", hits.Load())
	}

	fail.Store(false)
	current = base.Add(negativeTTL + time.Minute)
	if rec := get(t, proxy, URLFor("example.org")); !bytes.Equal(rec.Body.Bytes(), icon) {
		t.Fatal("expired negative entry should refetch the real icon")
	}
}

func TestIconCacheDeduplicatesSharedBodies(t *testing.T) {
	cache := newIconCache(maxCacheBytes, maxCacheEntries)
	icon := []byte("shared-icon-bytes")
	expires := clock().Add(time.Hour)

	cache.put("a.example", icon, "image/png", expires)
	cache.put("b.example", icon, "image/png", expires)
	cache.put("c.example", []byte("different"), "image/png", expires)

	if len(cache.bodies) != 2 {
		t.Fatalf("unique bodies = %d, want 2 (shared icon deduplicated)", len(cache.bodies))
	}
	if cache.totalBytes != len(icon)+len("different") {
		t.Fatalf("total bytes = %d, want deduplicated sum", cache.totalBytes)
	}
	if body, _, ok := cache.get("b.example"); !ok || !bytes.Equal(body, icon) {
		t.Fatal("deduplicated host lost its icon")
	}
}

func TestIconCacheEvictsLeastRecentlyUsedByBytes(t *testing.T) {
	// Budget holds two 8-byte bodies; the third insert evicts the LRU host.
	cache := newIconCache(16, maxCacheEntries)
	expires := clock().Add(time.Hour)

	cache.put("old.example", []byte("11111111"), "image/png", expires)
	cache.put("mid.example", []byte("22222222"), "image/png", expires)
	// Touch the oldest so recency protects it and "mid" becomes the victim.
	if _, _, ok := cache.get("old.example"); !ok {
		t.Fatal("touch miss")
	}
	cache.put("new.example", []byte("33333333"), "image/png", expires)

	if _, _, ok := cache.get("mid.example"); ok {
		t.Fatal("least-recently-used entry survived byte eviction")
	}
	for _, host := range []string{"old.example", "new.example"} {
		if _, _, ok := cache.get(host); !ok {
			t.Fatalf("%s evicted despite recency", host)
		}
	}
	if cache.totalBytes > 16 {
		t.Fatalf("total bytes = %d, want within budget", cache.totalBytes)
	}
}

func TestIconCacheEntryBackstopBoundsNegativeEntries(t *testing.T) {
	cache := newIconCache(maxCacheBytes, 3)
	expires := clock().Add(time.Hour)
	for _, host := range []string{"a.example", "b.example", "c.example", "d.example"} {
		cache.put(host, nil, "image/png", expires)
	}
	if got := len(cache.hosts); got != 3 {
		t.Fatalf("host entries = %d, want the backstop of 3", got)
	}
	if _, _, ok := cache.get("a.example"); ok {
		t.Fatal("oldest negative entry survived the entry backstop")
	}
}

func TestIconCacheDropsExpiredOnGetAndReplacesOnPut(t *testing.T) {
	base := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	current := base
	oldClock := clock
	t.Cleanup(func() { clock = oldClock })
	clock = func() time.Time { return current }

	cache := newIconCache(maxCacheBytes, maxCacheEntries)
	cache.put("a.example", []byte("body-one"), "image/png", base.Add(time.Minute))
	current = base.Add(2 * time.Minute)
	if _, _, ok := cache.get("a.example"); ok {
		t.Fatal("expired entry served")
	}
	if cache.totalBytes != 0 || len(cache.bodies) != 0 {
		t.Fatalf("expired entry left bytes behind: %d", cache.totalBytes)
	}

	cache.put("a.example", []byte("body-one"), "image/png", current.Add(time.Hour))
	cache.put("a.example", []byte("body-two!"), "image/png", current.Add(time.Hour))
	if cache.totalBytes != len("body-two!") {
		t.Fatalf("replacing a host's icon leaked bytes: %d", cache.totalBytes)
	}
}

func TestMountSkipsNilClient(t *testing.T) {
	mux := http.NewServeMux()
	Mount(mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, URLFor("example.org"), nil,
	))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil client route status = %d, want 404", rec.Code)
	}

	mounted := http.NewServeMux()
	Mount(mounted, &http.Client{})
	rec = httptest.NewRecorder()
	mounted.ServeHTTP(rec, httptest.NewRequestWithContext(
		t.Context(), http.MethodGet, Path, nil,
	))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mounted route without host = %d, want 400", rec.Code)
	}
}
