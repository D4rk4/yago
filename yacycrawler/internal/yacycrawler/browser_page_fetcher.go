package yacycrawler

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

const BrowserContentType = "text/html; charset=utf-8"

type pageRenderer func(ctx context.Context, rawURL string) (string, error)

type BrowserPageFetcher struct {
	render  pageRenderer
	timeout time.Duration
}

func NewBrowserPageFetcher(userAgent string, timeout time.Duration) (*BrowserPageFetcher, func()) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent(userAgent),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	fetcher := &BrowserPageFetcher{
		render:  chromedpRenderer(allocCtx),
		timeout: timeout,
	}
	return fetcher, allocCancel
}

func chromedpRenderer(allocCtx context.Context) pageRenderer {
	return func(ctx context.Context, rawURL string) (string, error) {
		tabCtx, cancel := chromedp.NewContext(allocCtx)
		defer cancel()
		stop := context.AfterFunc(ctx, cancel)
		defer stop()

		var content string
		err := chromedp.Run(tabCtx,
			chromedp.Navigate(rawURL),
			chromedp.OuterHTML("html", &content, chromedp.ByQuery),
		)
		if err != nil {
			return "", fmt.Errorf("chromedp run %s: %w", rawURL, err)
		}
		return content, nil
	}
}

func (f *BrowserPageFetcher) Fetch(ctx context.Context, rawURL string) (FetchedPage, error) {
	if f.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.timeout)
		defer cancel()
	}
	content, err := f.render(ctx, rawURL)
	if err != nil {
		return FetchedPage{}, fmt.Errorf("browser fetch %s: %w", rawURL, err)
	}
	return FetchedPage{
		URL:         rawURL,
		ContentType: BrowserContentType,
		Body:        []byte(content),
	}, nil
}
