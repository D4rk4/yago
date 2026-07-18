package main

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawldelay"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlermetrics"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

func assembleCrawlerPace(
	ctx context.Context,
	crawl CrawlConfig,
	checkpoint *frontiercheckpoint.FrontierCheckpoint,
	metrics *crawlermetrics.Metrics,
) (*crawldelay.AdaptivePace, error) {
	hostPace, err := crawldelay.NewHostPace(crawl.CrawlDelay, crawl.HostCacheSize)
	if err != nil {
		return nil, fmt.Errorf("create crawl pace: %w", err)
	}
	pace, err := newCrawlerAdaptivePace(hostPace, crawl.HostCacheSize, metrics)
	if err != nil {
		return nil, fmt.Errorf("create adaptive crawl pace: %w", err)
	}
	if err := restoreCrawlerHostPaces(ctx, checkpoint, pace); err != nil {
		return nil, err
	}

	return pace, nil
}
