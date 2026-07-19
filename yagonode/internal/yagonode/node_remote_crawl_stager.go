package yagonode

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
)

const (
	remoteCrawlStagingFailureMessage = "remote crawl staging failed"
	remoteCrawlStagingTimeout        = 2 * time.Second
	maximumRemoteCrawlStagingBacklog = 1024
)

type asynchronousRemoteCrawlStager struct {
	orders chan yagocrawlcontract.CrawlOrder
	sink   remoteCrawlOrderObserver
}

func newRemoteCrawlBrokerOrderStager(
	ctx context.Context,
	broker *remotecrawl.Broker,
	capacity int,
) remoteCrawlOrderObserver {
	if broker == nil {
		return nil
	}

	return newRemoteCrawlOrderStager(ctx, broker, capacity)
}

func newRemoteCrawlOrderStager(
	ctx context.Context,
	sink remoteCrawlOrderObserver,
	capacity int,
) remoteCrawlOrderObserver {
	if sink == nil {
		return nil
	}
	capacity = min(max(capacity, 1), maximumRemoteCrawlStagingBacklog)
	stager := &asynchronousRemoteCrawlStager{
		orders: make(chan yagocrawlcontract.CrawlOrder, capacity),
		sink:   sink,
	}
	go stager.run(ctx)

	return stager
}

func (s *asynchronousRemoteCrawlStager) StageOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("stage remote crawl order: %w", err)
	}
	order.Provenance = slices.Clone(order.Provenance)
	order.Requests = slices.Clone(order.Requests)
	select {
	case <-ctx.Done():
		return fmt.Errorf("stage remote crawl order: %w", ctx.Err())
	case s.orders <- order:
		return nil
	default:
		return fmt.Errorf("remote crawl staging backlog is full")
	}
}

func (s *asynchronousRemoteCrawlStager) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case order := <-s.orders:
			stageCtx, cancel := context.WithTimeout(
				context.WithoutCancel(ctx),
				remoteCrawlStagingTimeout,
			)
			err := s.sink.StageOrder(stageCtx, order)
			cancel()
			if err != nil {
				slog.WarnContext(
					ctx,
					remoteCrawlStagingFailureMessage,
					slog.Bool("localOrderRetained", true),
					slog.Any("error", err),
				)
			}
		}
	}
}
