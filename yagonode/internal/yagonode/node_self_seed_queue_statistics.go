package yagonode

import (
	"context"
	"log/slog"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
)

const (
	selfSeedCrawlBacklogUnavailableMessage = "self seed crawl backlog unavailable"
	selfSeedRemoteWorkUnavailableMessage   = "self seed remote crawl work unavailable"
)

type remoteCrawlPendingSource interface {
	PendingCount(context.Context) (int, error)
}

type selfSeedQueueStatistics struct {
	mu          sync.RWMutex
	crawl       func(context.Context) (crawlbroker.QueueDepth, error)
	crawlBound  bool
	remoteCrawl remoteCrawlPendingSource
}

func newSelfSeedQueueStatistics(remoteCrawl remoteCrawlPendingSource) *selfSeedQueueStatistics {
	return &selfSeedQueueStatistics{remoteCrawl: remoteCrawl}
}

func (s *selfSeedQueueStatistics) bindCrawl(runtime crawlProcess) {
	s.bindCrawlDepth(crawlQueueProbe(runtime))
}

func (s *selfSeedQueueStatistics) bindCrawlDepth(
	probe func(context.Context) (crawlbroker.QueueDepth, error),
) {
	s.mu.Lock()
	s.crawl = probe
	s.crawlBound = true
	s.mu.Unlock()
}

func (s *selfSeedQueueStatistics) SeedQueueStatistics(
	ctx context.Context,
) nodestatus.SeedQueueStatistics {
	statistics := nodestatus.SeedQueueStatistics{OfferedKnown: true}
	if s.remoteCrawl != nil {
		offered, err := s.remoteCrawl.PendingCount(ctx)
		if err != nil {
			slog.WarnContext(ctx, selfSeedRemoteWorkUnavailableMessage, slog.Any("error", err))
			statistics.OfferedKnown = false
		} else {
			statistics.Offered = offered
		}
	}

	s.mu.RLock()
	crawl := s.crawl
	bound := s.crawlBound
	s.mu.RUnlock()
	if !bound {
		return statistics
	}
	statistics.NoticedKnown = true
	if crawl == nil {
		return statistics
	}
	depth, err := crawl(ctx)
	if err != nil {
		slog.WarnContext(ctx, selfSeedCrawlBacklogUnavailableMessage, slog.Any("error", err))
		statistics.NoticedKnown = false

		return statistics
	}
	statistics.Noticed = depth.Outstanding()

	return statistics
}
