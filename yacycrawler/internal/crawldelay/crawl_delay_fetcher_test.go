package crawldelay_test

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawldelay"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
)

type pageSourceFunc func(context.Context, *url.URL) (pagefetch.FetchedPage, error)

func (f pageSourceFunc) Fetch(ctx context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
	return f(ctx, target)
}

func countingSource(calls *int) pageSourceFunc {
	return func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
		*calls++
		return pagefetch.FetchedPage{URL: target}, nil
	}
}

func newFetcher(
	t *testing.T,
	inner pagefetch.PageSource,
	delay time.Duration,
	size int,
) *crawldelay.CrawlDelayFetcher {
	t.Helper()
	fetcher, err := crawldelay.NewCrawlDelayFetcher(inner, delay, size)
	if err != nil {
		t.Fatalf("new fetcher: %v", err)
	}
	return fetcher
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsed
}

func TestCrawlDelayPacesPerHost(t *testing.T) {
	var calls int
	fetcher := newFetcher(t, countingSource(&calls), 40*time.Millisecond, 8)
	ctx := context.Background()

	start := time.Now()
	for range 3 {
		if _, err := fetcher.Fetch(ctx, mustParse(t, "https://example.com/page")); err != nil {
			t.Fatalf("fetch: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Errorf("elapsed %v, want at least 80ms for 3 paced fetches", elapsed)
	}
	if calls != 3 {
		t.Errorf("inner calls = %d, want 3", calls)
	}
}

func TestCrawlDelayPacesIndependentHosts(t *testing.T) {
	fetcher := newFetcher(t, countingSource(new(int)), time.Second, 8)
	ctx := context.Background()

	start := time.Now()
	if _, err := fetcher.Fetch(ctx, mustParse(t, "https://a.example/x")); err != nil {
		t.Fatalf("fetch a: %v", err)
	}
	if _, err := fetcher.Fetch(ctx, mustParse(t, "https://b.example/x")); err != nil {
		t.Fatalf("fetch b: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("distinct hosts should not pace each other, elapsed %v", elapsed)
	}
}

func TestCrawlDelayRejectsCancelledContext(t *testing.T) {
	fetcher := newFetcher(t, countingSource(new(int)), time.Hour, 8)
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := fetcher.Fetch(ctx, mustParse(t, "https://example.com/first")); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	cancel()
	if _, err := fetcher.Fetch(ctx, mustParse(t, "https://example.com/second")); err == nil {
		t.Error("expected cancelled context to abort the paced wait")
	}
}
