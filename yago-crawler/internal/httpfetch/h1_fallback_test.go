package httpfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
)

func TestIsHTTP2StreamError(t *testing.T) {
	cases := map[string]bool{
		`Get "https://x": stream error: stream ID 539; INTERNAL_ERROR; received from peer`: true,
		`Get "https://x": http2: server sent GOAWAY and closed the connection`:             true,
		`Get "https://x": http2: timeout awaiting response headers`:                        true,
		`Get "https://x": dial tcp: connection refused`:                                    false,
		`Get "https://x": context deadline exceeded`:                                       false,
		"": false,
	}
	for text, want := range cases {
		var err error
		if text != "" {
			err = errors.New(text)
		}
		if got := IsHTTP2StreamError(err); got != want {
			t.Fatalf("IsHTTP2StreamError(%q) = %v, want %v", text, got, want)
		}
	}
}

type countingTransport struct {
	calls int
	fail  bool
}

func (t *countingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.calls++
	if t.fail {
		return nil, fmt.Errorf(
			"stream error: stream ID %d; INTERNAL_ERROR; received from peer", t.calls,
		)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(strings.NewReader("<html>ok</html>")),
		Request:    request,
	}, nil
}

// TestFetcherFallsBackToHTTP1OnStreamErrors pins CRAWL-18: the first fetch
// retries through the h1 client after an h2 stream reset, and the host is
// remembered — the second fetch never touches the h2 client.
func TestFetcherFallsBackToHTTP1OnStreamErrors(t *testing.T) {
	h2 := &countingTransport{fail: true}
	h1 := &countingTransport{}
	fetcher := NewPageFetcher(&http.Client{Transport: h2}, "", 0).
		WithHTTP1Fallback(&http.Client{Transport: h1})
	target, _ := url.Parse("https://hostile.example/page")

	page, err := fetcher.Fetch(context.Background(), target)
	if err != nil {
		t.Fatalf("first fetch must succeed through the fallback: %v", err)
	}
	if !strings.HasPrefix(page.ContentType, "text/html") || h2.calls != 1 || h1.calls != 1 {
		t.Fatalf("first fetch: h2=%d h1=%d type=%q", h2.calls, h1.calls, page.ContentType)
	}

	if _, err := fetcher.Fetch(context.Background(), target); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if h2.calls != 1 || h1.calls != 2 {
		t.Fatalf("downgraded host must skip h2: h2=%d h1=%d", h2.calls, h1.calls)
	}
}

// TestFetcherWithoutFallbackKeepsTheError pins the unarmed path: no h1 client
// means the stream error propagates as before.
func TestFetcherWithoutFallbackKeepsTheError(t *testing.T) {
	fetcher := NewPageFetcher(&http.Client{Transport: &countingTransport{fail: true}}, "", 0)
	target, _ := url.Parse("https://hostile.example/page")
	if _, err := fetcher.Fetch(context.Background(), target); err == nil ||
		!errors.Is(err, err) || !strings.Contains(err.Error(), "INTERNAL_ERROR") {
		t.Fatalf("unarmed fetcher must propagate: %v", err)
	}
	var rejected pagefetch.FetchedPage
	_ = rejected
}

func TestHostDowngradesExpiryAndCap(t *testing.T) {
	downgrades := newHostDowngrades()
	now := time.Unix(1_800_000_000, 0)
	downgrades.now = func() time.Time { return now }

	downgrades.Mark("a.example")
	if !downgrades.Active("a.example") || downgrades.Active("b.example") {
		t.Fatal("mark/active basics broken")
	}
	now = now.Add(h1DowngradeTTL + time.Second)
	if downgrades.Active("a.example") {
		t.Fatal("expired downgrade must clear")
	}

	for i := range h1DowngradeCap {
		downgrades.Mark(fmt.Sprintf("h%04d.example", i))
	}
	downgrades.Mark("overflow.example")
	if downgrades.Active("overflow.example") {
		t.Fatal("a full table with fresh entries must skip new marks")
	}
	now = now.Add(h1DowngradeTTL + time.Second)
	downgrades.Mark("overflow.example")
	if !downgrades.Active("overflow.example") {
		t.Fatal("sweeping expired entries must make room")
	}
}
