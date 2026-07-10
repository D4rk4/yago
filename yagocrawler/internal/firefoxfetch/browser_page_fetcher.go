package firefoxfetch

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagoegress"
)

const (
	BrowserContentType = "text/html; charset=utf-8"
	// Marionette's WebDriver:ExecuteScript wraps each snippet in a function body,
	// so a script must `return` its value (unlike a chromedp expression). Reading
	// the whole document element at call time avoids a stale node reference if the
	// page navigates (redirect, meta refresh, client-side navigation) mid-fetch.
	jsDocumentContentType = "return document.contentType"
	jsDocumentOuterHTML   = "return document.documentElement.outerHTML"
)

type pageRenderer func(ctx context.Context, rawURL string) (renderedPage, error)

type renderedPage struct {
	url         string
	content     string
	contentType string
}

type BrowserPageFetcher struct {
	render   pageRenderer
	timeout  time.Duration
	maxBytes int64
}

// BrowserLaunch selects how the slow-path browser is launched so the crawler
// runs the same binary under Docker, systemd on bare metal, and a Debian/RPM
// package. ExecPath names the Firefox binary; an empty value falls back to
// discovery on PATH (firefox-esr, then firefox). Sandbox keeps Firefox's own
// content-process sandbox; it defaults off because the container image and
// hosts that restrict unprivileged user namespaces cannot start it, and an
// operator on a host that supports it opts back in.
type BrowserLaunch struct {
	UserAgent string
	Timeout   time.Duration
	MaxBytes  int64
	ExecPath  string
	Sandbox   bool
}

// NewBrowserPageFetcher builds the slow-path fetcher backed by one long-lived
// headless Firefox reached over Marionette. It starts the egress-guarded
// forward proxy the browser routes through and returns a close function that
// tears down the Firefox session and the proxy. The Firefox process itself is
// launched lazily on the first fetch, so a crawler whose fast path never gets
// bot-walled never spends a browser.
func NewBrowserPageFetcher(
	launch BrowserLaunch,
	guard yagoegress.Guard,
) (*BrowserPageFetcher, func(), error) {
	proxy, err := startGuardedForwardProxy(
		dialFunc(yagoegress.PreferIPv4(nil, (&net.Dialer{Control: guard.DialControl}).DialContext)),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start browser egress proxy: %w", err)
	}
	manager := &firefoxManager{
		launch:   launch,
		proxyURL: proxy.url,
		timeout:  launch.Timeout,
		start:    startFirefoxSession,
	}
	fetcher := &BrowserPageFetcher{
		render:   manager.render,
		timeout:  launch.Timeout,
		maxBytes: launch.MaxBytes,
	}
	closeFetcher := func() {
		manager.close()
		proxy.Close()
	}

	return fetcher, closeFetcher, nil
}

// browserSession is one live browser the manager drives: navigate-and-extract,
// a liveness check, and teardown. launchFirefox returns the real Firefox-backed
// implementation; tests supply a fake to exercise the manager's relaunch and
// teardown paths without spawning a browser.
type browserSession interface {
	render(ctx context.Context, rawURL string, timeout time.Duration) (renderedPage, error)
	alive() bool
	close()
}

// startFirefoxSession is the launch seam so NewBrowserPageFetcher wires the real
// browser while tests inject a fake session factory.
func startFirefoxSession(launch BrowserLaunch, proxyURL string) (browserSession, error) {
	session, err := launchFirefox(launch, proxyURL)
	if err != nil {
		return nil, err
	}

	return session, nil
}

// firefoxManager owns the crawler's single long-lived Firefox session and
// serializes every render through it. Marionette carries one command at a time,
// and holding one process — instead of launching a browser per fetch, as the
// chromedp path did — is the point of the design; a session whose command
// stream errors is torn down and rebuilt on the next fetch.
type firefoxManager struct {
	launch   BrowserLaunch
	proxyURL string
	timeout  time.Duration
	start    func(BrowserLaunch, string) (browserSession, error)

	mu      sync.Mutex
	session browserSession
}

func (m *firefoxManager) render(ctx context.Context, rawURL string) (renderedPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, err := m.ensureSession()
	if err != nil {
		return renderedPage{}, err
	}
	page, err := session.render(ctx, rawURL, m.timeout)
	if err != nil {
		// A failed command may have desynchronized the Marionette stream, or the
		// process may be gone; discard the session so the next fetch starts clean.
		session.close()
		m.session = nil

		return renderedPage{}, err
	}

	return page, nil
}

func (m *firefoxManager) ensureSession() (browserSession, error) {
	if m.session != nil && m.session.alive() {
		return m.session, nil
	}
	if m.session != nil {
		m.session.close()
		m.session = nil
	}
	session, err := m.start(m.launch, m.proxyURL)
	if err != nil {
		return nil, fmt.Errorf("launch firefox: %w", err)
	}
	m.session = session

	return session, nil
}

func (m *firefoxManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session != nil {
		m.session.close()
		m.session = nil
	}
}

func (f *BrowserPageFetcher) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		defer cancel()
	}
	rendered, err := f.render(ctx, target.String())
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("browser fetch %s: %w", target, err)
	}
	contentType := rendered.contentType
	if strings.TrimSpace(contentType) == "" {
		contentType = BrowserContentType
	}
	if !pagefetch.AllowedContentType(contentType) {
		return pagefetch.FetchedPage{}, fmt.Errorf(
			"browser fetch %s content type %q: %w",
			target,
			contentType,
			pagefetch.ErrUnsupportedContentType,
		)
	}
	body := []byte(rendered.content)
	if f.maxBytes > 0 && int64(len(body)) > f.maxBytes {
		body = body[:f.maxBytes]
	}
	final, err := url.Parse(rendered.url)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("browser final url %s: %w", rendered.url, err)
	}

	return pagefetch.FetchedPage{
		URL:         final,
		ContentType: contentType,
		Body:        body,
	}, nil
}
