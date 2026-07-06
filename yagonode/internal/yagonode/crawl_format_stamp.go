package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlformats"
)

// formatStampingQueue stamps the operator's shared document-format toggles into
// every dispatched crawl profile — operator crawls, recrawls, and swarm seeds
// alike — so one console setting governs which format families all crawls parse.
type formatStampingQueue struct {
	inner interface {
		PublishOnce(
			ctx context.Context,
			key string,
			order yagocrawlcontract.CrawlOrder,
		) (bool, error)
	}
	formats *crawlformats.Store
}

func (q formatStampingQueue) PublishOnce(
	ctx context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	order.Profile.Formats = q.formats.Current(ctx)

	//nolint:wrapcheck // transparent decorator over the durable queue.
	return q.inner.PublishOnce(ctx, key, order)
}
