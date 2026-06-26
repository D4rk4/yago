package robots_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/robots"
)

const testUserAgent = "yacy-rwi-node-crawler/0.1 (+https://yacy.net)"

type pageSourceFunc func(context.Context, *url.URL) (pagefetch.FetchedPage, error)

func (f pageSourceFunc) Fetch(ctx context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
	return f(ctx, target)
}

func deliveringSource() pageSourceFunc {
	return func(_ context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
		return pagefetch.FetchedPage{URL: target}, nil
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsed
}

func robotsServer(t *testing.T, rule string, robotsHits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			if robotsHits != nil {
				atomic.AddInt32(robotsHits, 1)
			}
			if _, err := w.Write([]byte(rule)); err != nil {
				t.Errorf("write robots: %v", err)
			}
			return
		}
		w.Header().Set("Content-Type", "text/html")
	}))
}

func newFetcher(
	t *testing.T,
	inner pagefetch.PageSource,
	client *http.Client,
	size int,
) *robots.RobotsAdmissionFetcher {
	t.Helper()
	fetcher, err := robots.NewRobotsAdmissionFetcher(inner, client, testUserAgent, size)
	if err != nil {
		t.Fatalf("new fetcher: %v", err)
	}
	return fetcher
}

func TestRobotsAdmissionBlocksDisallowedPath(t *testing.T) {
	server := robotsServer(t, "User-agent: *\nDisallow: /private\n", nil)
	defer server.Close()
	fetcher := newFetcher(t, deliveringSource(), server.Client(), 8)

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, server.URL+"/private/secret"),
	); !errors.Is(
		err,
		pagefetch.ErrPageRejected,
	) {
		t.Errorf("err = %v, want ErrPageRejected", err)
	}
	page, err := fetcher.Fetch(context.Background(), mustParse(t, server.URL+"/public"))
	if err != nil {
		t.Fatalf("allow public: %v", err)
	}
	if page.URL.String() != server.URL+"/public" {
		t.Errorf("page not delegated: %+v", page)
	}
}

func TestRobotsAdmissionFetchesRobotsOncePerHost(t *testing.T) {
	var hits int32
	server := robotsServer(t, "User-agent: *\nDisallow: /private\n", &hits)
	defer server.Close()
	fetcher := newFetcher(t, deliveringSource(), server.Client(), 8)

	for range 3 {
		if _, err := fetcher.Fetch(
			context.Background(),
			mustParse(t, server.URL+"/public"),
		); err != nil {
			t.Fatalf("fetch: %v", err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("robots fetches = %d, want 1", got)
	}
}

func TestRobotsAdmissionAllowsOnFetchFailure(t *testing.T) {
	fetcher := newFetcher(t, deliveringSource(), http.DefaultClient, 8)
	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "http://127.0.0.1:0/page"),
	); err != nil {
		t.Errorf("unreachable robots should allow, got %v", err)
	}
}

func TestRobotsAdmissionReFetchesAfterEviction(t *testing.T) {
	var hits int32
	server := robotsServer(t, "User-agent: *\nDisallow: /private\n", &hits)
	defer server.Close()
	other := robotsServer(t, "User-agent: *\nAllow: /\n", nil)
	defer other.Close()
	fetcher := newFetcher(t, deliveringSource(), server.Client(), 1)

	steps := []string{server.URL + "/public", other.URL + "/public", server.URL + "/public"}
	for _, u := range steps {
		if _, err := fetcher.Fetch(context.Background(), mustParse(t, u)); err != nil {
			t.Fatalf("fetch %s: %v", u, err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("robots fetches for evicted host = %d, want 2", got)
	}
}
