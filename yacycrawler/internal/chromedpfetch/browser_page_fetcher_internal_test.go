package chromedpfetch

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsed
}

func TestBrowserPageFetcherReturnsRenderedBody(t *testing.T) {
	fetcher := &BrowserPageFetcher{
		render: func(_ context.Context, rawURL string) (renderedPage, error) {
			return renderedPage{
				url:     rawURL,
				content: "<html><body>" + rawURL + "</body></html>",
			}, nil
		},
		timeout: time.Second,
	}

	page, err := fetcher.Fetch(context.Background(), mustParse(t, "http://example.com/"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.URL.String() != "http://example.com/" {
		t.Errorf("url = %q", page.URL)
	}
	if page.ContentType != BrowserContentType {
		t.Errorf("content type = %q", page.ContentType)
	}
	if string(page.Body) != "<html><body>http://example.com/</body></html>" {
		t.Errorf("body = %q", page.Body)
	}
}

func TestBrowserPageFetcherPropagatesRenderError(t *testing.T) {
	sentinel := errors.New("render failed")
	fetcher := &BrowserPageFetcher{
		render: func(context.Context, string) (renderedPage, error) {
			return renderedPage{}, sentinel
		},
	}

	_, err := fetcher.Fetch(context.Background(), mustParse(t, "http://example.com/"))
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want %v", err, sentinel)
	}
}

func TestBrowserPageFetcherAppliesTimeout(t *testing.T) {
	fetcher := &BrowserPageFetcher{
		render: func(ctx context.Context, _ string) (renderedPage, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("expected deadline on render context")
			}
			return renderedPage{url: "http://example.com/", content: "ok"}, nil
		},
		timeout: time.Second,
	}

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "http://example.com/"),
	); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
}

func TestNewBrowserPageFetcherBuildsFetcher(t *testing.T) {
	fetcher, cancel := NewBrowserPageFetcher("agent/1.0", "http://proxy:4750", time.Second, 4<<20)
	defer cancel()

	if fetcher == nil || fetcher.render == nil {
		t.Fatal("expected configured fetcher")
	}
}

func TestBrowserPageFetcherReturnsFinalURL(t *testing.T) {
	fetcher := &BrowserPageFetcher{
		render: func(context.Context, string) (renderedPage, error) {
			return renderedPage{
				url:     "http://example.com/final",
				content: "<html></html>",
			}, nil
		},
	}

	page, err := fetcher.Fetch(context.Background(), mustParse(t, "http://example.com/start"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.URL.String() != "http://example.com/final" {
		t.Errorf("url = %q", page.URL)
	}
}

func TestBrowserPageFetcherCapsRenderedBody(t *testing.T) {
	fetcher := &BrowserPageFetcher{
		render: func(context.Context, string) (renderedPage, error) {
			return renderedPage{
				url:     "http://example.com/",
				content: "abcdef",
			}, nil
		},
		maxBytes: 3,
	}

	page, err := fetcher.Fetch(context.Background(), mustParse(t, "http://example.com/"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(page.Body) != "abc" {
		t.Errorf("body = %q", page.Body)
	}
}
