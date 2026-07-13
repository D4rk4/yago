package firefoxfetch

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagoegress"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return parsed
}

// fakeSession is a browserSession stand-in so the manager's lifecycle can be
// tested without launching Firefox.
type fakeSession struct {
	renderFunc func(ctx context.Context, rawURL string, timeout time.Duration) (renderedPage, error)
	aliveVal   bool
	closed     int
}

func (f *fakeSession) render(
	ctx context.Context,
	rawURL string,
	timeout time.Duration,
) (renderedPage, error) {
	return f.renderFunc(ctx, rawURL, timeout)
}

func (f *fakeSession) alive() bool { return f.aliveVal }

func (f *fakeSession) close() { f.closed++ }

func staticRender(
	finalURL string,
) func(context.Context, string, time.Duration) (renderedPage, error) {
	return func(context.Context, string, time.Duration) (renderedPage, error) {
		return renderedPage{url: finalURL, content: "<html></html>"}, nil
	}
}

func TestFirefoxManagerReusesOneSession(t *testing.T) {
	session := &fakeSession{aliveVal: true, renderFunc: staticRender("http://example.com/")}
	starts := 0
	manager := &firefoxManager{
		start: func(context.Context, BrowserLaunch, string) (browserSession, error) {
			starts++
			return session, nil
		},
	}

	for i := 0; i < 3; i++ {
		if _, err := manager.render(context.Background(), "http://example.com/"); err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
	}
	if starts != 1 {
		t.Fatalf("launches = %d, want 1 (the session is reused)", starts)
	}
}

func TestFirefoxManagerRelaunchesAfterRenderError(t *testing.T) {
	sentinel := errors.New("marionette stream broke")
	broken := &fakeSession{
		aliveVal:   true,
		renderFunc: func(context.Context, string, time.Duration) (renderedPage, error) { return renderedPage{}, sentinel },
	}
	healthy := &fakeSession{aliveVal: true, renderFunc: staticRender("http://example.com/next")}
	starts := 0
	manager := &firefoxManager{
		start: func(context.Context, BrowserLaunch, string) (browserSession, error) {
			starts++
			if starts == 1 {
				return broken, nil
			}
			return healthy, nil
		},
	}

	if _, err := manager.render(
		context.Background(),
		"http://example.com/",
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("first render error = %v, want %v", err, sentinel)
	}
	if broken.closed != 1 {
		t.Fatalf("broken session closed %d times, want 1", broken.closed)
	}
	page, err := manager.render(context.Background(), "http://example.com/next")
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if page.url != "http://example.com/next" {
		t.Fatalf("url = %q", page.url)
	}
	if starts != 2 {
		t.Fatalf("launches = %d, want 2 (relaunch after the stream broke)", starts)
	}
}

func TestFirefoxManagerRelaunchesDeadSession(t *testing.T) {
	first := &fakeSession{aliveVal: true, renderFunc: staticRender("http://example.com/a")}
	second := &fakeSession{aliveVal: true, renderFunc: staticRender("http://example.com/b")}
	starts := 0
	manager := &firefoxManager{
		start: func(context.Context, BrowserLaunch, string) (browserSession, error) {
			starts++
			if starts == 1 {
				return first, nil
			}
			return second, nil
		},
	}

	if _, err := manager.render(context.Background(), "http://example.com/a"); err != nil {
		t.Fatalf("first render: %v", err)
	}
	first.aliveVal = false // the browser process exited between fetches

	page, err := manager.render(context.Background(), "http://example.com/b")
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if page.url != "http://example.com/b" {
		t.Fatalf("url = %q", page.url)
	}
	if first.closed != 1 {
		t.Fatalf("dead session closed %d times, want 1", first.closed)
	}
	if starts != 2 {
		t.Fatalf("launches = %d, want 2 (a dead session is replaced)", starts)
	}
}

func TestFirefoxManagerReturnsLaunchError(t *testing.T) {
	boom := errors.New("no firefox binary")
	manager := &firefoxManager{
		start: func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return nil, boom
		},
	}
	if _, err := manager.render(
		context.Background(),
		"http://example.com/",
	); !errors.Is(
		err,
		boom,
	) {
		t.Fatalf("error = %v, want %v", err, boom)
	}
}

func TestFirefoxManagerCircuitBreakerCoolsAndProbes(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	starts := 0
	manager := &firefoxManager{
		launch: BrowserLaunch{FailureThreshold: 2},
		start: func(context.Context, BrowserLaunch, string) (browserSession, error) {
			starts++

			return nil, errors.New("firefox unavailable")
		},
		now: func() time.Time { return now },
	}
	if _, err := manager.render(t.Context(), "https://example.org/first"); err == nil {
		t.Fatal("first failure returned nil")
	}
	if !manager.retryAfter.IsZero() {
		t.Fatalf("first failure retry time = %s, want zero", manager.retryAfter)
	}
	if _, err := manager.render(t.Context(), "https://example.org/second"); err == nil {
		t.Fatal("threshold failure returned nil")
	}
	wantRetry := now.Add(pagefetch.DefaultBrowserBreakerCooldown)
	if !manager.retryAfter.Equal(wantRetry) {
		t.Fatalf("retry time = %s, want %s", manager.retryAfter, wantRetry)
	}
	if _, err := manager.render(t.Context(), "https://example.org/cooling"); err == nil ||
		!strings.Contains(err.Error(), "cooling down") {
		t.Fatalf("cooldown error = %v", err)
	}
	now = wantRetry
	if _, err := manager.render(t.Context(), "https://example.org/probe"); err == nil {
		t.Fatal("failed probe returned nil")
	}
	if manager.failures != 0 ||
		!manager.retryAfter.Equal(now.Add(pagefetch.DefaultBrowserBreakerCooldown)) {
		t.Fatalf("failed probe state = failures %d retry %s", manager.failures, manager.retryAfter)
	}
	if starts != 3 {
		t.Fatalf("launches = %d, want 3", starts)
	}
}

func TestFirefoxManagerDoesNotLaunchForExpiredRender(t *testing.T) {
	starts := 0
	manager := &firefoxManager{
		start: func(context.Context, BrowserLaunch, string) (browserSession, error) {
			starts++
			return nil, errors.New("unexpected launch")
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.render(ctx, "http://example.com/"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if starts != 0 {
		t.Fatalf("launches = %d, want 0", starts)
	}
}

func TestFirefoxManagerCloseTearsDownSession(t *testing.T) {
	session := &fakeSession{aliveVal: true, renderFunc: staticRender("http://example.com/")}
	manager := &firefoxManager{
		start: func(context.Context, BrowserLaunch, string) (browserSession, error) {
			return session, nil
		},
	}
	if _, err := manager.render(context.Background(), "http://example.com/"); err != nil {
		t.Fatalf("render: %v", err)
	}
	manager.close()
	if session.closed != 1 {
		t.Fatalf("session closed %d times, want 1", session.closed)
	}
	if manager.session != nil {
		t.Fatal("manager still holds a session after close")
	}
	if _, err := manager.render(
		t.Context(),
		"http://example.com/",
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("render after close = %v, want cancellation", err)
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

func TestBrowserPageFetcherReportsCapturedContentType(t *testing.T) {
	fetcher := &BrowserPageFetcher{
		render: func(_ context.Context, rawURL string) (renderedPage, error) {
			return renderedPage{
				url:         rawURL,
				content:     "<html></html>",
				contentType: "application/xhtml+xml",
			}, nil
		},
	}

	page, err := fetcher.Fetch(context.Background(), mustParse(t, "http://example.com/"))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if page.ContentType != "application/xhtml+xml" {
		t.Errorf("content type = %q, want the captured type", page.ContentType)
	}
}

func TestBrowserPageFetcherRejectsDisallowedContentType(t *testing.T) {
	fetcher := &BrowserPageFetcher{
		render: func(_ context.Context, rawURL string) (renderedPage, error) {
			return renderedPage{
				url:         rawURL,
				content:     "%PDF-1.7 ...",
				contentType: "application/pdf",
			}, nil
		},
	}

	_, err := fetcher.Fetch(context.Background(), mustParse(t, "http://example.com/doc.pdf"))
	if !errors.Is(err, pagefetch.ErrUnsupportedContentType) {
		t.Fatalf("error = %v, want ErrUnsupportedContentType", err)
	}
	if !errors.Is(err, pagefetch.ErrPageRejected) {
		t.Fatalf("error = %v, must stay a page rejection", err)
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

func TestNewBrowserPageFetcherBuildsFetcher(t *testing.T) {
	fetcher, cancel, err := NewBrowserPageFetcher(
		BrowserLaunch{UserAgent: "agent/1.0", Timeout: time.Second, MaxBytes: 4 << 20},
		yagoegress.NewGuard(false),
	)
	if err != nil {
		t.Fatalf("new fetcher: %v", err)
	}
	defer cancel()

	if fetcher == nil || fetcher.render == nil {
		t.Fatal("expected configured fetcher")
	}
}

func TestNewBrowserPageFetcherHonorsSandboxAndExecPath(t *testing.T) {
	fetcher, cancel, err := NewBrowserPageFetcher(
		BrowserLaunch{
			UserAgent: "agent/1.0",
			Timeout:   time.Second,
			MaxBytes:  4 << 20,
			ExecPath:  "/usr/bin/firefox-esr",
			Sandbox:   true,
		},
		yagoegress.NewGuard(false),
	)
	if err != nil {
		t.Fatalf("new fetcher: %v", err)
	}
	defer cancel()

	if fetcher == nil || fetcher.render == nil {
		t.Fatal("expected configured fetcher with sandbox and explicit binary")
	}
}

func TestNewBrowserPageFetcherFailsWhenProxyCannotListen(t *testing.T) {
	restore := listenBrowserProxy
	t.Cleanup(func() { listenBrowserProxy = restore })
	listenBrowserProxy = func() (net.Listener, error) {
		return nil, errors.New("listen refused")
	}

	if _, _, err := NewBrowserPageFetcher(
		BrowserLaunch{UserAgent: "agent/1.0", Timeout: time.Second, MaxBytes: 4 << 20},
		yagoegress.NewGuard(false),
	); err == nil {
		t.Fatal("expected error when the browser proxy cannot listen")
	}
}
