package yagonode

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
)

const (
	selfSeedRemoteWorkUnavailableMessage = "self seed remote crawl work unavailable"
)

type remoteCrawlPendingSource interface {
	PendingCount(context.Context) (int, error)
}

type selfSeedQueueStatistics struct {
	remoteCrawl remoteCrawlPendingSource
}

func newSelfSeedQueueStatistics(remoteCrawl remoteCrawlPendingSource) *selfSeedQueueStatistics {
	return &selfSeedQueueStatistics{remoteCrawl: remoteCrawl}
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

	return statistics
}
