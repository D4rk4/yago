package yagonode

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
)

const remoteCrawlStagingSkippedMessage = "remote crawl staging skipped"

type remoteCrawlOrderObserver interface {
	StageOrder(context.Context, yagocrawlcontract.CrawlOrder) error
}

type remoteCrawlObservedOrderQueue struct {
	inner    crawldispatch.CrawlOrderQueue
	observer remoteCrawlOrderObserver
}

type keylessCrawlOrderPublisher struct {
	queue crawldispatch.CrawlOrderQueue
}

func (p keylessCrawlOrderPublisher) Publish(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
) error {
	_, err := p.queue.PublishOnce(ctx, "", order)
	if err != nil {
		return fmt.Errorf("publish crawl order: %w", err)
	}

	return nil
}

func (q remoteCrawlObservedOrderQueue) PublishOnce(
	ctx context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	duplicate, err := q.inner.PublishOnce(ctx, key, order)
	if err != nil {
		return duplicate, fmt.Errorf("publish observed crawl order: %w", err)
	}
	if duplicate || q.observer == nil {
		return duplicate, nil
	}
	if err := q.observer.StageOrder(ctx, order); err != nil {
		slog.WarnContext(
			ctx,
			remoteCrawlStagingSkippedMessage,
			slog.Bool("localOrderRetained", true),
			slog.Any("error", err),
		)
	}

	return false, nil
}

func attachRemoteCrawlOrders(runtime crawlProcess, observer remoteCrawlOrderObserver) {
	if observer == nil {
		return
	}
	attached, ok := runtime.(interface {
		useRemoteCrawlObserver(remoteCrawlOrderObserver)
	})
	if ok {
		attached.useRemoteCrawlObserver(observer)
	}
}
