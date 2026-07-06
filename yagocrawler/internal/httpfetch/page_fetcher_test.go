package httpfetch_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/httpfetch"
	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type readCloserFunc struct {
	read  func([]byte) (int, error)
	close func() error
}

func (f readCloserFunc) Read(bytes []byte) (int, error) {
	return f.read(bytes)
}

func (f readCloserFunc) Close() error {
	return f.close()
}

func TestPageFetcherReturnsHTMLPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "crawler/1.0" {
			t.Fatalf("user agent = %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>hello</body></html>"))
	}))
	defer server.Close()

	page, err := httpfetch.NewPageFetcher(
		server.Client(),
		"crawler/1.0",
		0,
	).Fetch(context.Background(), mustParse(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.URL.String() != server.URL {
		t.Fatalf("url = %q", page.URL)
	}
	if page.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", page.ContentType)
	}
	if string(page.Body) != "<html><body>hello</body></html>" {
		t.Fatalf("body = %q", page.Body)
	}
}

func TestPageFetcherReturnsFinalRedirectURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	page, err := httpfetch.NewPageFetcher(
		server.Client(),
		"",
		0,
	).Fetch(context.Background(), mustParse(t, server.URL+"/start"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.URL.String() != server.URL+"/final" {
		t.Fatalf("url = %q", page.URL)
	}
}

func TestPageFetcherUsesTargetWhenResponseRequestIsMissing(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(
		func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/html"}},
				Body: io.NopCloser(
					strings.NewReader("<html><body>ok</body></html>"),
				),
			}, nil
		},
	)}
	target := mustParse(t, "https://example.com/page")

	page, err := httpfetch.NewPageFetcher(client, "", 0).Fetch(context.Background(), target)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.URL.String() != target.String() {
		t.Fatalf("url = %q", page.URL)
	}
}

func TestPageFetcherDetectsHTMLContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html><body>detected</body></html>"))
	}))
	defer server.Close()

	page, err := httpfetch.NewPageFetcher(
		server.Client(),
		"",
		0,
	).Fetch(context.Background(), mustParse(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("content type = %q", page.ContentType)
	}
}

func TestPageFetcherCapsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>abcdef</html>"))
	}))
	defer server.Close()

	page, err := httpfetch.NewPageFetcher(
		server.Client(),
		"",
		9,
	).Fetch(context.Background(), mustParse(t, server.URL))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(page.Body) != "<html>abc" {
		t.Fatalf("body = %q", page.Body)
	}
}

func TestPageFetcherRejectsNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := httpfetch.NewPageFetcher(
		server.Client(),
		"",
		0,
	).Fetch(context.Background(), mustParse(t, server.URL))
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("error = %v, want page rejected", err)
	}
	// A status rejection is not a content-type rejection: the browser fallback
	// must still be given a chance to pass the wall.
	if errors.Is(err, pagefetch.ErrUnsupportedContentType) {
		t.Fatalf("error = %v, status rejection must not be an unsupported-content-type", err)
	}
}

func TestPageFetcherRejectsNonHTMLContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	_, err := httpfetch.NewPageFetcher(
		server.Client(),
		"",
		0,
	).Fetch(context.Background(), mustParse(t, server.URL))
	if !errors.Is(err, pagefetch.ErrUnsupportedContentType) {
		t.Fatalf("error = %v, want unsupported content type", err)
	}
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("error = %v, unsupported content type must remain a page rejection", err)
	}
}

func TestPageFetcherRejectsEmptyContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil
	}))
	defer server.Close()

	_, err := httpfetch.NewPageFetcher(
		server.Client(),
		"",
		0,
	).Fetch(context.Background(), mustParse(t, server.URL))
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("error = %v, want page rejected", err)
	}
}

func TestPageFetcherRejectsEmptyBodyWithoutContentType(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(
		func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    request,
			}, nil
		},
	)}

	_, err := httpfetch.NewPageFetcher(
		client,
		"",
		0,
	).Fetch(context.Background(), mustParse(t, "https://example.com/"))
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("error = %v, want page rejected", err)
	}
}

func TestPageFetcherReturnsBodyReadError(t *testing.T) {
	sentinel := errors.New("read failed")
	client := &http.Client{Transport: roundTripFunc(
		func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/html"}},
				Body: readCloserFunc{
					read:  func([]byte) (int, error) { return 0, sentinel },
					close: func() error { return nil },
				},
				Request: request,
			}, nil
		},
	)}

	_, err := httpfetch.NewPageFetcher(
		client,
		"",
		0,
	).Fetch(context.Background(), mustParse(t, "https://example.com/"))
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestPageFetcherReturnsRequestAndNetworkErrors(t *testing.T) {
	for _, target := range []*url.URL{
		{Scheme: "http", Host: "\n"},
		mustParse(t, "http://127.0.0.1:1/"),
	} {
		_, err := httpfetch.NewPageFetcher(
			http.DefaultClient,
			"",
			0,
		).Fetch(context.Background(), target)
		if err == nil {
			t.Fatalf("target %#v should fail", target)
		}
	}
}

func TestNewPageFetcherAcceptsNilClient(t *testing.T) {
	if httpfetch.NewPageFetcher(nil, "", 0) == nil {
		t.Fatal("fetcher is nil")
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

// TestPageFetcherSignalsThrottleWithRetryAfter: 429/503 become typed throttle
// errors carrying the Retry-After wish; other bad statuses stay plain
// rejections.
func TestPageFetcherSignalsThrottleWithRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/throttled":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
		case "/unavailable":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	fetcher := httpfetch.NewPageFetcher(server.Client(), "", 1<<20)

	_, err := fetcher.Fetch(context.Background(), mustParse(t, server.URL+"/throttled"))
	throttled, ok := pagefetch.AsThrottled(err)
	if !ok || throttled.Status != http.StatusTooManyRequests ||
		throttled.RetryAfter != 2*time.Minute {
		t.Fatalf("throttle = %#v, %v", throttled, ok)
	}

	_, err = fetcher.Fetch(context.Background(), mustParse(t, server.URL+"/unavailable"))
	if throttled, ok = pagefetch.AsThrottled(err); !ok || throttled.RetryAfter != 0 {
		t.Fatalf("503 throttle = %#v, %v", throttled, ok)
	}

	_, err = fetcher.Fetch(context.Background(), mustParse(t, server.URL+"/missing"))
	if _, ok = pagefetch.AsThrottled(err); ok || !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("404 error = %v", err)
	}
}
