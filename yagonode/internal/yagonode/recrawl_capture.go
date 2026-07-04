package yagonode

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
	"github.com/D4rk4/yago/yagonode/internal/recrawlfrontier"
)

const msgRecrawlProfileRecordFailed = "recrawl profile record failed"

// profileRecordingQueue records each dispatched order's crawl profile in the
// recrawl frontier before enqueuing it, so that when the order's pages are later
// ingested their recrawl interval is known and a due URL can be turned back into a
// faithful crawl order. Recording is best-effort: a failure is logged and never
// blocks the dispatch, since the crawl must still go out.
type profileRecordingQueue struct {
	inner    crawldispatch.CrawlOrderQueue
	frontier *recrawlfrontier.Frontier
}

func (q profileRecordingQueue) PublishOnce(
	ctx context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	if err := q.frontier.RecordProfile(ctx, order.Profile); err != nil {
		slog.WarnContext(ctx, msgRecrawlProfileRecordFailed,
			slog.String("profile", order.Profile.Handle), slog.Any("error", err))
	}

	duplicate, err := q.inner.PublishOnce(ctx, key, order)
	if err != nil {
		return duplicate, fmt.Errorf("publish recorded crawl order: %w", err)
	}

	return duplicate, nil
}
