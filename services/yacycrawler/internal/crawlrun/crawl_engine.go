package crawlrun

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlcapability"
)

const msgOrderDropped = "crawl order dropped"

type Engine struct {
	observer  crawlcapability.RunProgress
	traversal OrderTraversal
}

func NewEngine(observer crawlcapability.RunProgress, traversal OrderTraversal) *Engine {
	return &Engine{observer: observer, traversal: traversal}
}

func (e *Engine) Run(ctx context.Context, deliveries <-chan crawlcapability.DeliveredOrder) error {
	for {
		select {
		case <-ctx.Done():
			return contextError(ctx)
		case delivery, ok := <-deliveries:
			if !ok {
				return nil
			}
			e.settleDelivery(ctx, delivery, e.crawl(ctx, delivery))
			if err := contextError(ctx); err != nil {
				return err
			}
		}
	}
}

func (e *Engine) crawl(ctx context.Context, delivery crawlcapability.DeliveredOrder) error {
	e.observer.OrderReceived()
	if err := e.traversal.Traverse(ctx, delivery); err != nil {
		return fmt.Errorf("traverse order %s: %w", delivery.Order.OrderID, err)
	}
	return nil
}

func (e *Engine) settleDelivery(
	ctx context.Context,
	delivery crawlcapability.DeliveredOrder,
	crawlErr error,
) {
	if crawlErr != nil {
		e.observer.OrderRedelivered()
		if err := delivery.Retry(ctx); err != nil {
			slog.WarnContext(ctx, msgOrderDropped,
				slog.String("order", delivery.Order.OrderID),
				slog.Any("error", err),
			)
		}
		return
	}
	if err := delivery.Ack(ctx); err != nil {
		slog.WarnContext(ctx, msgOrderDropped,
			slog.String("order", delivery.Order.OrderID),
			slog.Any("error", err),
		)
		return
	}
	e.observer.OrderCompleted()
}
