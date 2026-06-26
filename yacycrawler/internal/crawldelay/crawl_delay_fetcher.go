package crawldelay

import (
	"context"
	"fmt"
	"net/url"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/time/rate"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/pagefetch"
)

const DefaultCrawlDelay = 1 * time.Second

type CrawlDelayFetcher struct {
	inner    pagefetch.PageSource
	delay    time.Duration
	limiters *lru.Cache[string, *rate.Limiter]
}

func NewCrawlDelayFetcher(
	inner pagefetch.PageSource,
	delay time.Duration,
	hostCacheSize int,
) (*CrawlDelayFetcher, error) {
	limiters, err := lru.New[string, *rate.Limiter](hostCacheSize)
	if err != nil {
		return nil, fmt.Errorf("crawl delay host cache: %w", err)
	}
	return &CrawlDelayFetcher{inner: inner, delay: delay, limiters: limiters}, nil
}

func (f *CrawlDelayFetcher) Fetch(
	ctx context.Context,
	target *url.URL,
) (pagefetch.FetchedPage, error) {
	if err := f.limiter(target.Host).Wait(ctx); err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("rate limit wait: %w", err)
	}
	page, err := f.inner.Fetch(ctx, target)
	if err != nil {
		return pagefetch.FetchedPage{}, fmt.Errorf("inner fetch: %w", err)
	}
	return page, nil
}

func (f *CrawlDelayFetcher) limiter(host string) *rate.Limiter {
	if limiter, ok := f.limiters.Get(host); ok {
		return limiter
	}
	limiter := rate.NewLimiter(rate.Every(f.delay), 1)
	f.limiters.Add(host, limiter)
	return limiter
}
