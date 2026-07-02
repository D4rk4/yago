package chromedpfetch

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/D4rk4/yago/yacyegress"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsed
}

func restoreChromedpSeams(
	t *testing.T,
	newTabContext func(context.Context, ...chromedp.ContextOption) (context.Context, context.CancelFunc),
	runActions func(context.Context, ...chromedp.Action) error,
) {
	t.Helper()
	t.Cleanup(func() {
		newChromedpTabContext = newTabContext
		runChromedpActions = runActions
	})
}

func TestChromedpRendererReturnsRenderedPage(t *testing.T) {
	restoreChromedpSeams(t, newChromedpTabContext, runChromedpActions)
	newChromedpTabContext = func(ctx context.Context, _ ...chromedp.ContextOption) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}
	runChromedpActions = func(context.Context, ...chromedp.Action) error {
		return nil
	}

	rendered, err := chromedpRenderer(
		context.Background(),
	)(
		context.Background(),
		"http://example.com/",
	)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if rendered.url != "http://example.com/" {
		t.Fatalf("url = %q", rendered.url)
	}
}

func TestChromedpRendererReturnsRunError(t *testing.T) {
	restoreChromedpSeams(t, newChromedpTabContext, runChromedpActions)
	sentinel := errors.New("run failed")
	newChromedpTabContext = func(ctx context.Context, _ ...chromedp.ContextOption) (context.Context, context.CancelFunc) {
		return context.WithCancel(ctx)
	}
	runChromedpActions = func(context.Context, ...chromedp.Action) error {
		return sentinel
	}

	_, err := chromedpRenderer(context.Background())(context.Background(), "http://example.com/")
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
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
	fetcher, cancel, err := NewBrowserPageFetcher(
		"agent/1.0",
		yacyegress.NewGuard(false),
		time.Second,
		4<<20,
	)
	if err != nil {
		t.Fatalf("new fetcher: %v", err)
	}
	defer cancel()

	if fetcher == nil || fetcher.render == nil {
		t.Fatal("expected configured fetcher")
	}
}

func TestNewBrowserPageFetcherFailsWhenProxyCannotListen(t *testing.T) {
	restore := listenBrowserProxy
	t.Cleanup(func() { listenBrowserProxy = restore })
	listenBrowserProxy = func() (net.Listener, error) {
		return nil, errors.New("listen refused")
	}

	if _, _, err := NewBrowserPageFetcher(
		"agent/1.0",
		yacyegress.NewGuard(false),
		time.Second,
		4<<20,
	); err == nil {
		t.Fatal("expected error when the browser proxy cannot listen")
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

func TestBrowserPageFetcherRejectsBadFinalURL(t *testing.T) {
	fetcher := &BrowserPageFetcher{
		render: func(context.Context, string) (renderedPage, error) {
			return renderedPage{url: "http://[bad", content: "<html></html>"}, nil
		},
	}

	if _, err := fetcher.Fetch(
		context.Background(),
		mustParse(t, "http://example.com/"),
	); err == nil {
		t.Fatal("bad final URL should fail")
	}
}
