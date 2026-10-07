package yagonode

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawlbroker"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

const msgCrawlOrderClearFailed = "pending crawl order clear failed"

type crawlOrderClearSource struct {
	queue *crawlbroker.DurableOrderQueue
}

func newCrawlOrderClearSource(runtime crawlProcess) adminui.CrawlOrderClearSource {
	live, ok := runtime.(*crawlRuntime)
	if !ok || live == nil || live.broker == nil || live.broker.Orders == nil {
		return nil
	}

	return crawlOrderClearSource{queue: live.broker.Orders}
}

func (source crawlOrderClearSource) ClearPendingOrders(ctx context.Context) (int, error) {
	removed, err := source.queue.ClearPendingOrders(ctx)
	if err != nil {
		slog.WarnContext(ctx, msgCrawlOrderClearFailed,
			tracectx.ServerSpanAttribute(ctx),
			slog.Int("removed", removed),
		)
		return removed, fmt.Errorf("clear pending crawler orders: %w", err)
	}

	return removed, nil
}
