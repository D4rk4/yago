package chromedpfetch

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/D4rk4/yago/yagocrawler/internal/pagefetch"
	"github.com/D4rk4/yago/yagoegress"
)

const (
	BrowserContentType    = "text/html; charset=utf-8"
	jsDocumentContentType = "document.contentType"
)

var newChromedpTabContext = chromedp.NewContext

var runChromedpActions = chromedp.Run

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

// BrowserLaunch selects how the slow-path browser is launched so the crawler runs
// the same binary under Docker, systemd on bare metal, and a Debian package.
// ExecPath names the browser binary; an empty value falls back to discovery on
// PATH (the bundled headless-shell in the container image, chromium on a Debian
// host). Sandbox keeps Chrome's own renderer sandbox; it defaults off because the
// container image and modern hosts that restrict unprivileged user namespaces
// cannot start the sandbox, and an operator on a host that supports it opts back in.
type BrowserLaunch struct {
	UserAgent string
	Timeout   time.Duration
	MaxBytes  int64
	ExecPath  string
	Sandbox   bool
}

func NewBrowserPageFetcher(
	launch BrowserLaunch,
	guard yagoegress.Guard,
) (*BrowserPageFetcher, func(), error) {
	proxy, err := startGuardedForwardProxy((&net.Dialer{Control: guard.DialControl}).DialContext)
	if err != nil {
		return nil, nil, fmt.Errorf("start browser egress proxy: %w", err)
	}
	// disable-dev-shm-usage keeps Chrome off the small default /dev/shm so a large
	// page cannot crash the tab; it is harmless on every target.
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent(launch.UserAgent),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	if !launch.Sandbox {
		opts = append(opts, chromedp.NoSandbox)
	}
	if launch.ExecPath != "" {
		opts = append(opts, chromedp.ExecPath(launch.ExecPath))
	}
	opts = append(opts, proxyExecAllocatorOptions(proxy.url)...)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	fetcher := &BrowserPageFetcher{
		render:   chromedpRenderer(allocCtx),
		timeout:  launch.Timeout,
		maxBytes: launch.MaxBytes,
	}
	closeFetcher := func() {
		allocCancel()
		proxy.Close()
	}

	return fetcher, closeFetcher, nil
}

func chromedpRenderer(allocCtx context.Context) pageRenderer {
	return func(ctx context.Context, rawURL string) (renderedPage, error) {
		tabCtx, cancel := newChromedpTabContext(allocCtx)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()

		var content, contentType string
		finalURL := rawURL
		err := runChromedpActions(tabCtx,
			chromedp.Navigate(rawURL),
			chromedp.OuterHTML("html", &content, chromedp.ByQuery),
			chromedp.Location(&finalURL),
			chromedp.Evaluate(jsDocumentContentType, &contentType),
		)
		if err != nil {
			return renderedPage{}, fmt.Errorf("chromedp run %s: %w", rawURL, err)
		}
		return renderedPage{url: finalURL, content: content, contentType: contentType}, nil
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
