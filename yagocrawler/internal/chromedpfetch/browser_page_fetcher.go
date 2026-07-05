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

func NewBrowserPageFetcher(
	userAgent string,
	guard yagoegress.Guard,
	timeout time.Duration,
	maxBytes int64,
) (*BrowserPageFetcher, func(), error) {
	proxy, err := startGuardedForwardProxy((&net.Dialer{Control: guard.DialControl}).DialContext)
	if err != nil {
		return nil, nil, fmt.Errorf("start browser egress proxy: %w", err)
	}
	// The crawler is a disposable, egress-guarded container that the host isolates,
	// and current Linux distros disable the unprivileged user namespaces Chrome's
	// zygote sandbox needs, so the browser aborts with "No usable sandbox" without
	// these flags. Dropping Chrome's own sandbox is safe here because the container
	// is the isolation boundary; disable-dev-shm-usage keeps Chrome off the small
	// default /dev/shm so a large page cannot crash the tab.
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent(userAgent),
		chromedp.NoSandbox,
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	opts = append(opts, proxyExecAllocatorOptions(proxy.url)...)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	fetcher := &BrowserPageFetcher{
		render:   chromedpRenderer(allocCtx),
		timeout:  timeout,
		maxBytes: maxBytes,
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
