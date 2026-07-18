package firefoxfetch

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/pagefetch"
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
	UserAgent        string
	Timeout          time.Duration
	MaxBytes         int64
	ExecPath         string
	Sandbox          bool
	Sessions         int
	FailureThreshold int
	FailureCooldown  time.Duration
}

func NewBrowserPageFetcher(
	launch BrowserLaunch,
	guard yagoegress.Guard,
	observeBrowserSlotAcquisitionDeadline ...func(),
) (*BrowserPageFetcher, func(), error) {
	proxy, err := startGuardedForwardProxy(
		dialFunc(yagoegress.PreferIPv4(nil, (&net.Dialer{Control: guard.DialControl}).DialContext)),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start browser egress proxy: %w", err)
	}
	pool := newFirefoxPool(
		launch,
		proxy.url,
		startFirefoxSession,
		observeBrowserSlotAcquisitionDeadline...,
	)
	fetcher := &BrowserPageFetcher{
		render:   pool.render,
		timeout:  launch.Timeout,
		maxBytes: launch.MaxBytes,
	}
	closeFetcher := func() {
		pool.close()
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
func startFirefoxSession(
	ctx context.Context,
	launch BrowserLaunch,
	proxyURL string,
) (browserSession, error) {
	session, err := launchFirefox(ctx, launch, proxyURL)
	if err != nil {
		return nil, err
	}

	return session, nil
}

type firefoxManager struct {
	launch   BrowserLaunch
	proxyURL string
	timeout  time.Duration
	start    func(context.Context, BrowserLaunch, string) (browserSession, error)
	now      func() time.Time

	mu         sync.Mutex
	session    browserSession
	closed     bool
	failures   int
	retryAfter time.Time
}

func (m *firefoxManager) render(ctx context.Context, rawURL string) (renderedPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return renderedPage{}, fmt.Errorf("firefox session pool closed: %w", context.Canceled)
	}
	if err := ctx.Err(); err != nil {
		return renderedPage{}, fmt.Errorf("wait for firefox session: %w", err)
	}
	now := time.Now
	if m.now != nil {
		now = m.now
	}
	probing := false
	if !m.retryAfter.IsZero() {
		if current := now(); current.Before(m.retryAfter) {
			return renderedPage{}, fmt.Errorf(
				"firefox session cooling down until %s",
				m.retryAfter.UTC().Format(time.RFC3339),
			)
		}
		m.retryAfter = time.Time{}
		probing = true
	}

	session, err := m.ensureSession(ctx)
	if err != nil {
		m.recordFailure(now(), probing)
		return renderedPage{}, err
	}
	page, err := session.render(ctx, rawURL, m.timeout)
	if err != nil {
		// A failed command may have desynchronized the Marionette stream, or the
		// process may be gone; discard the session so the next fetch starts clean.
		session.close()
		m.session = nil
		m.recordFailure(now(), probing)

		return renderedPage{}, err
	}
	m.failures = 0
	m.retryAfter = time.Time{}

	return page, nil
}

func (m *firefoxManager) ensureSession(ctx context.Context) (browserSession, error) {
	if m.session != nil && m.session.alive() {
		return m.session, nil
	}
	if m.session != nil {
		m.session.close()
		m.session = nil
	}
	session, err := m.start(ctx, m.launch, m.proxyURL)
	if err != nil {
		return nil, fmt.Errorf("launch firefox: %w", err)
	}
	m.session = session

	return session, nil
}

func (m *firefoxManager) recordFailure(now time.Time, probing bool) {
	if m.launch.FailureThreshold <= 0 {
		return
	}
	m.failures++
	if !probing && m.failures < m.launch.FailureThreshold {
		return
	}
	cooldown := m.launch.FailureCooldown
	if cooldown <= 0 {
		cooldown = pagefetch.DefaultBrowserBreakerCooldown
	}
	m.failures = 0
	m.retryAfter = now.Add(cooldown)
}

func (m *firefoxManager) coolingUntil() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.retryAfter
}

func (m *firefoxManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
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
