package chromedpfetch

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
)

const BrowserContentType = "text/html; charset=utf-8"

type pageRenderer func(ctx context.Context, rawURL string) (renderedPage, error)

type renderedPage struct {
	url     string
	content string
}

type BrowserPageFetcher struct {
	render   pageRenderer
	timeout  time.Duration
	maxBytes int64
}

func NewBrowserPageFetcher(
	userAgent string,
	timeout time.Duration,
	maxBytes int64,
) (*BrowserPageFetcher, func()) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent(userAgent),
	)
	opts = append(opts, proxyExecAllocatorOptions(os.Getenv)...)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	fetcher := &BrowserPageFetcher{
		render:   chromedpRenderer(allocCtx),
		timeout:  timeout,
		maxBytes: maxBytes,
	}
	return fetcher, allocCancel
}

func chromedpRenderer(allocCtx context.Context) pageRenderer {
	return func(ctx context.Context, rawURL string) (renderedPage, error) {
		tabCtx, cancel := chromedp.NewContext(allocCtx)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()

		var content string
		finalURL := rawURL
		err := chromedp.Run(tabCtx,
			chromedp.Navigate(rawURL),
			chromedp.OuterHTML("html", &content, chromedp.ByQuery),
			chromedp.Location(&finalURL),
		)
		if err != nil {
			return renderedPage{}, fmt.Errorf("chromedp run %s: %w", rawURL, err)
		}
		return renderedPage{url: finalURL, content: content}, nil
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
		ContentType: BrowserContentType,
		Body:        body,
	}, nil
}
