package httpfetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/httpfetch"
)

const testUserAgent = "test-agent (+https://example.test)"

func proxyURL(t *testing.T, handler http.HandlerFunc) (*url.URL, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return parsed, server.Close
}

func TestFetchSuccess(t *testing.T) {
	var gotUserAgent string
	proxy, closeFn := proxyURL(t, func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>hi</html>"))
	})
	defer closeFn()

	outcome, err := httpfetch.New(proxy, testUserAgent, 1<<20, time.Second).
		Fetch(context.Background(), "http://target.example/page")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if outcome.Status != crawlcapability.FetchSucceeded {
		t.Fatalf("kind = %v", outcome.Status)
	}
	if string(outcome.Body) != "<html>hi</html>" {
		t.Fatalf("body = %q", outcome.Body)
	}
	if outcome.ContentType != "text/html" {
		t.Fatalf("content type = %q", outcome.ContentType)
	}
	if gotUserAgent != testUserAgent {
		t.Fatalf("user agent = %q, want %q", gotUserAgent, testUserAgent)
	}
}

func TestFetchTruncatesOversizedBody(t *testing.T) {
	proxy, closeFn := proxyURL(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0123456789"))
	})
	defer closeFn()

	outcome, err := httpfetch.New(proxy, testUserAgent, 4, time.Second).
		Fetch(context.Background(), "http://target.example/big")
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Truncated || len(outcome.Body) != 4 {
		t.Fatalf("expected truncation to 4 bytes, got %d truncated=%v",
			len(outcome.Body), outcome.Truncated)
	}
}

func TestFetchStatusMapping(t *testing.T) {
	cases := map[int]crawlcapability.FetchStatus{
		http.StatusTooManyRequests:            crawlcapability.FetchDeferred,
		http.StatusServiceUnavailable:         crawlcapability.FetchDeferred,
		http.StatusForbidden:                  crawlcapability.FetchCeased,
		http.StatusUnauthorized:               crawlcapability.FetchCeased,
		http.StatusUnavailableForLegalReasons: crawlcapability.FetchCeased,
		http.StatusNotFound:                   crawlcapability.FetchNotAPage,
		http.StatusInternalServerError:        crawlcapability.FetchTransient,
	}
	for status, wantKind := range cases {
		proxy, closeFn := proxyURL(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		})
		outcome, err := httpfetch.New(proxy, testUserAgent, 1<<20, time.Second).
			Fetch(context.Background(), "http://target.example/x")
		closeFn()
		if err != nil {
			t.Fatalf("status %d: %v", status, err)
		}
		if outcome.Status != wantKind {
			t.Errorf("status %d: kind = %v, want %v", status, outcome.Status, wantKind)
		}
	}
}

func TestFetchDeferHonorsRetryAfter(t *testing.T) {
	proxy, closeFn := proxyURL(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer closeFn()

	outcome, _ := httpfetch.New(proxy, testUserAgent, 1<<20, time.Second).
		Fetch(context.Background(), "http://target.example/x")
	if outcome.DeferFor != 42*time.Second {
		t.Fatalf("defer = %v, want 42s", outcome.DeferFor)
	}
}

func TestFetchReadsXRobotsTag(t *testing.T) {
	proxy, closeFn := proxyURL(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		_, _ = w.Write([]byte("hi"))
	})
	defer closeFn()

	outcome, _ := httpfetch.New(proxy, testUserAgent, 1<<20, time.Second).
		Fetch(context.Background(), "http://target.example/x")
	if !outcome.RefusesIndexing || !outcome.RefusesLinkDiscovery {
		t.Fatalf("x-robots-tag not parsed: %+v", outcome)
	}
}

func TestFetchTransientOnProxyFailure(t *testing.T) {
	proxy, _ := url.Parse("http://127.0.0.1:1")
	outcome, err := httpfetch.New(proxy, testUserAgent, 1<<20, time.Second).
		Fetch(context.Background(), "http://target.example/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != crawlcapability.FetchTransient {
		t.Fatalf("kind = %v, want transient", outcome.Status)
	}
}

func TestFetchCancelledContext(t *testing.T) {
	proxy, _ := url.Parse("http://127.0.0.1:1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := httpfetch.New(proxy, testUserAgent, 1<<20, time.Second).
		Fetch(ctx, "http://target.example/x")
	if err == nil {
		t.Fatal("cancelled context should error")
	}
}
