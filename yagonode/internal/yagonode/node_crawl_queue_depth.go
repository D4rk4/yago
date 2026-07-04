package yagonode

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
)

const crawlQueueDepthUnavailableMessage = "crawl queue depth unavailable"

// crawlQueueDepthSource reads the broker's crawl order backlog for the crawl
// queue depth metric and the Performance console tile. A nil probe (crawling
// disabled) reports zero so both consumers render without a live crawl runtime.
type crawlQueueDepthSource struct {
	probe func(context.Context) (crawlbroker.QueueDepth, error)
}

func (s crawlQueueDepthSource) outstanding(ctx context.Context) int {
	if s.probe == nil {
		return 0
	}
	depth, err := s.probe(ctx)
	if err != nil {
		slog.WarnContext(ctx, crawlQueueDepthUnavailableMessage, slog.Any("error", err))

		return 0
	}

	return depth.Outstanding()
}
