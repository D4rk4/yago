package robots_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
	"github.com/D4rk4/yago/yago-crawler/internal/robots"
)

const testUserAgent = "yago-crawler/0.1 (+https://github.com/D4rk4/yago/)"

type pageSourceFunc func(context.Context, *url.URL) (pagefetch.FetchedPage, error)

func (f pageSourceFunc) Fetch(ctx context.Context, target *url.URL) (pagefetch.FetchedPage, error) {
	return f(ctx, target)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type readCloserFunc struct {
	read  func([]byte) (int, error)
	close func() error
}

func (b readCloserFunc) Read(p []byte) (int, error) {
	return b.read(p)
}

func (b readCloserFunc) Close() error {
	return b.close()
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
			if _, err := strings.NewReader(rule).WriteTo(w); err != nil {
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

func TestRobotsAdmissionBlocksOnFetchFailure(t *testing.T) {
	fetcher := newFetcher(t, deliveringSource(), http.DefaultClient, 8)
	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "http://127.0.0.1:0/page"),
	); !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Errorf("unreachable robots error = %v, want ErrPageRejected", err)
	}
}

func TestNewRobotsAdmissionFetcherRejectsInvalidCacheSize(t *testing.T) {
	if _, err := robots.NewRobotsAdmissionFetcher(
		deliveringSource(),
		http.DefaultClient,
		testUserAgent,
		0,
	); err == nil {
		t.Fatal("expected error for zero cache size")
	}
}

func TestRobotsAdmissionPropagatesInnerFetchError(t *testing.T) {
	server := robotsServer(t, "User-agent: *\nAllow: /\n", nil)
	defer server.Close()
	sentinel := errors.New("origin down")
	fetcher := newFetcher(t, pageSourceFunc(
		func(context.Context, *url.URL) (pagefetch.FetchedPage, error) {
			return pagefetch.FetchedPage{}, sentinel
		},
	), server.Client(), 8)

	_, err := fetcher.Fetch(context.Background(), mustParse(t, server.URL+"/public"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("Fetch error = %v, want %v", err, sentinel)
	}
}

func TestRobotsAdmissionBlocksOnBadRobotsRequestURL(t *testing.T) {
	fetcher := newFetcher(t, deliveringSource(), http.DefaultClient, 8)

	if _, err := fetcher.Fetch(
		context.Background(),
		&url.URL{Scheme: "http", Host: "bad host", Path: "/page"},
	); !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("bad robots request error = %v, want ErrPageRejected", err)
	}
}

func TestRobotsAdmissionBlocksOnRobotsReadError(t *testing.T) {
	sentinel := errors.New("read failed")
	client := &http.Client{Transport: roundTripFunc(
		func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: readCloserFunc{
					read:  func([]byte) (int, error) { return 0, sentinel },
					close: func() error { return nil },
				},
			}, nil
		},
	)}
	fetcher := newFetcher(t, deliveringSource(), client, 8)

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "http://example.com/page"),
	); !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("robots read error = %v, want ErrPageRejected", err)
	}
}

func TestRobotsAdmissionLimitsRobotsBody(t *testing.T) {
	const expectedBytes = 500 * 1024
	body := "User-agent: *\nDisallow: /private\n" + strings.Repeat("#", expectedBytes)
	var consumed strings.Builder
	client := &http.Client{Transport: roundTripFunc(
		func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(io.TeeReader(strings.NewReader(body), &consumed)),
			}, nil
		},
	)}
	fetcher := newFetcher(t, deliveringSource(), client, 8)

	_, err := fetcher.Fetch(context.Background(), mustParse(t, "http://example.com/private"))
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("Fetch error = %v, want ErrPageRejected", err)
	}
	if consumed.Len() != expectedBytes {
		t.Fatalf("robots body bytes read = %d, want %d", consumed.Len(), expectedBytes)
	}
}

func TestRobotsAdmissionAllowsOnUnexpectedRobotsStatus(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(
		func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusFound,
				Body:       io.NopCloser(strings.NewReader("redirect")),
			}, nil
		},
	)}
	fetcher := newFetcher(t, deliveringSource(), client, 8)

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "http://example.com/page"),
	); err != nil {
		t.Fatalf("unexpected robots status should fail open, got %v", err)
	}
}

func TestRobotsAdmissionLogsBodyCloseErrorAndUsesRules(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(
		func(*http.Request) (*http.Response, error) {
			reader := strings.NewReader("User-agent: *\nDisallow: /private\n")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body: readCloserFunc{
					read:  reader.Read,
					close: func() error { return errors.New("close failed") },
				},
			}, nil
		},
	)}
	fetcher := newFetcher(t, deliveringSource(), client, 8)

	_, err := fetcher.Fetch(context.Background(), mustParse(t, "http://example.com/private"))
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("Fetch error = %v, want ErrPageRejected", err)
	}
}

func TestRobotsAdmissionCachesFetchFailure(t *testing.T) {
	var robotsHits int32
	client := &http.Client{Transport: roundTripFunc(
		func(*http.Request) (*http.Response, error) {
			if atomic.AddInt32(&robotsHits, 1) == 1 {
				return nil, errors.New("transient network error")
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("User-agent: *\nDisallow: /private\n")),
			}, nil
		},
	)}
	fetcher := newFetcher(t, deliveringSource(), client, 8)

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "http://example.com/private"),
	); !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("transient robots error = %v, want ErrPageRejected", err)
	}

	_, err := fetcher.Fetch(context.Background(), mustParse(t, "http://example.com/private"))
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("second fetch = %v, want ErrPageRejected", err)
	}
	if got := atomic.LoadInt32(&robotsHits); got != 1 {
		t.Errorf("robots fetches = %d, want 1", got)
	}
}

// TestRobotsAdmissionSanitizesMalformedRobotsAndCaches is the regression guard
// for the re-fetch/log storm on a real-world quirk: a Crawl-delay before any
// User-agent line makes the strict parser reject the whole file. The sanitizer
// drops the stray leading directive so the Disallow is still honored, and the
// fetched body is cached so the malformed file is fetched once, not per page.
func TestRobotsAdmissionSanitizesMalformedRobotsAndCaches(t *testing.T) {
	var hits int32
	rule := "# public site\n\nCrawl-delay: 10\nUser-agent: *\nDisallow: /private\n"
	server := robotsServer(t, rule, &hits)
	defer server.Close()
	fetcher := newFetcher(t, deliveringSource(), server.Client(), 8)

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, server.URL+"/private/secret"),
	); !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("err = %v, want ErrPageRejected (sanitized Disallow honored)", err)
	}
	for range 3 {
		if _, err := fetcher.Fetch(
			context.Background(),
			mustParse(t, server.URL+"/public"),
		); err != nil {
			t.Fatalf("allow public: %v", err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf(
			"robots fetches = %d, want 1 (malformed robots must be cached, not re-fetched)",
			got,
		)
	}
}

// TestRobotsAdmissionCachesUnparseableRobotsAsAllowAll covers the fallback: a bad
// Crawl-delay value sits inside the group, so sanitizing the pre-group lines
// cannot fix it and both parse attempts fail. The crawler then allows the host
// (fail open) and caches that outcome, so an unparseable file is not re-fetched
// on every page either.
func TestRobotsAdmissionCachesUnparseableRobotsAsAllowAll(t *testing.T) {
	var hits int32
	rule := "User-agent: *\nCrawl-delay: notanumber\nDisallow: /private\n"
	server := robotsServer(t, rule, &hits)
	defer server.Close()
	fetcher := newFetcher(t, deliveringSource(), server.Client(), 8)

	for range 3 {
		if _, err := fetcher.Fetch(
			context.Background(),
			mustParse(t, server.URL+"/private/secret"),
		); err != nil {
			t.Fatalf("unparseable robots should fail open, got %v", err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("robots fetches = %d, want 1 (unparseable robots cached as allow-all)", got)
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
