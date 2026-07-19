package firefoxfetch

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
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
	pool     *firefoxPool
}

// BrowserLaunch selects how the slow-path browser is launched so the crawler
// runs the same binary under Docker, systemd on bare metal, and a Debian/RPM
// package. ExecPath names the Firefox binary; an empty value falls back to
// discovery on PATH (firefox-esr, then firefox). Sandbox keeps Firefox's own
// content-process sandbox; it defaults off because the container image and
// hosts that restrict unprivileged user namespaces cannot start it, and an
// operator on a host that supports it opts back in.
type BrowserLaunch struct {
	UserAgent          string
	Timeout            time.Duration
	MaxBytes           int64
	ExecPath           string
	Sandbox            bool
	Sessions           int
	FailureThreshold   int
	FailureCooldown    time.Duration
	MaxRedirects       int
	executableResolver func(string, bool) (string, error)
}

func NewBrowserPageFetcher(
	launch BrowserLaunch,
	guard yagoegress.Guard,
	observeBrowserSlotAcquisitionDeadline ...func(),
) (*BrowserPageFetcher, func(), error) {
	return newBrowserPageFetcher(
		launch,
		guard,
		browserPoolObservation{legacyDeadline: selectBrowserSlotAcquisitionDeadlineObserver(
			observeBrowserSlotAcquisitionDeadline,
		)},
	)
}

func NewBrowserPageFetcherWithPoolObservation(
	launch BrowserLaunch,
	guard yagoegress.Guard,
	observer BrowserPoolObserver,
) (*BrowserPageFetcher, func(), error) {
	return newBrowserPageFetcher(
		launch,
		guard,
		browserPoolObservation{observer: observer},
	)
}

func newBrowserPageFetcher(
	launch BrowserLaunch,
	guard yagoegress.Guard,
	observation browserPoolObservation,
) (*BrowserPageFetcher, func(), error) {
	executablePath, err := launch.firefoxExecutable(false)
	if err != nil {
		return nil, nil, err
	}
	launch.ExecPath = executablePath
	proxy, err := startGuardedForwardProxy(
		dialFunc(yagoegress.PreferIPv4(nil, (&net.Dialer{Control: guard.DialControl}).DialContext)),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start browser egress proxy: %w", err)
	}
	pool := newFirefoxPoolObserved(
		launch,
		proxy.url,
		startFirefoxSession,
		observation,
	)
	fetcher := &BrowserPageFetcher{
		render:   pool.render,
		timeout:  launch.Timeout,
		maxBytes: launch.MaxBytes,
		pool:     pool,
	}
	closeFetcher := func() {
		pool.close()
		proxy.Close()
	}

	return fetcher, closeFetcher, nil
}

func (launch BrowserLaunch) firefoxExecutable(required bool) (string, error) {
	if launch.executableResolver != nil {
		return launch.executableResolver(launch.ExecPath, required)
	}

	return resolveTrustedFirefoxExecutable(
		launch.ExecPath,
		required,
		operatingSystemFirefoxExecutableFilesystem(),
	)
}

func (f *BrowserPageFetcher) SetMaxRedirects(maximum int) {
	if f != nil && f.pool != nil {
		f.pool.setMaxRedirects(maximum)
	}
}

func (f *BrowserPageFetcher) SetSandbox(enabled bool) {
	if f != nil && f.pool != nil {
		f.pool.setSandbox(enabled)
	}
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

	maximumRedirects atomic.Int64
	redirectUpdate   atomic.Bool
	sandbox          atomic.Bool
	sandboxUpdate    atomic.Bool
	cooling          atomic.Bool
}

func (m *firefoxManager) render(ctx context.Context, rawURL string) (renderedPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applyMaxRedirects()
	m.applySandbox()
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
		m.cooling.Store(false)
		probing = true
	}

	session, err := m.ensureSession(ctx)
	if err != nil {
		m.recordFailure(now(), probing)
		return renderedPage{}, browserFailureError{reason: BrowserFailureLaunch, cause: err}
	}
	page, err := session.render(ctx, rawURL, m.timeout)
	if err != nil {
		// A failed command may have desynchronized the Marionette stream, or the
		// process may be gone; discard the session so the next fetch starts clean.
		session.close()
		m.session = nil
		m.recordFailure(now(), probing)

		return renderedPage{}, browserFailureError{reason: BrowserFailureRender, cause: err}
	}
	m.failures = 0
	m.retryAfter = time.Time{}
	m.cooling.Store(false)

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
	m.cooling.Store(true)
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

func (m *firefoxManager) setMaxRedirects(maximum int) {
	if maximum < 0 {
		return
	}
	m.maximumRedirects.Store(int64(maximum))
	m.redirectUpdate.Store(true)
}

func (m *firefoxManager) applyMaxRedirects() {
	if !m.redirectUpdate.Swap(false) {
		return
	}
	maximum := int(m.maximumRedirects.Load())
	if m.launch.MaxRedirects == maximum {
		return
	}
	m.launch.MaxRedirects = maximum
	if m.session != nil {
		m.session.close()
		m.session = nil
	}
}

func (m *firefoxManager) setSandbox(enabled bool) {
	m.sandbox.Store(enabled)
	m.sandboxUpdate.Store(true)
}

func (m *firefoxManager) applySandbox() {
	if !m.sandboxUpdate.Swap(false) {
		return
	}
	enabled := m.sandbox.Load()
	if m.launch.Sandbox == enabled {
		return
	}
	m.launch.Sandbox = enabled
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
