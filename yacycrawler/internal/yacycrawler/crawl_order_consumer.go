package yacycrawler

import (
	"context"
	"log/slog"
)

const (
	msgProfileRegisterFailed = "crawl profile registration failed"
	msgOrderAckFailed        = "crawl order ack failed"
	msgOrderNakFailed        = "crawl order nak failed"
	msgOrderTermFailed       = "crawl order term failed"
)

type CrawlOrderConsumer struct {
	orders   Receiver[CrawlOrderDelivery]
	registry *CrawlProfileRegistry
	frontier *Frontier
}

func NewCrawlOrderConsumer(
	orders Receiver[CrawlOrderDelivery],
	registry *CrawlProfileRegistry,
	frontier *Frontier,
) *CrawlOrderConsumer {
	return &CrawlOrderConsumer{orders: orders, registry: registry, frontier: frontier}
}

func (c *CrawlOrderConsumer) Run(ctx context.Context) {
	c.frontier.Hold()
	defer c.frontier.Release()
	for {
		select {
		case <-ctx.Done():
			return
		case delivery, ok := <-c.orders.Receive():
			if !ok {
				return
			}
			c.accept(ctx, delivery)
		}
	}
}

func (c *CrawlOrderConsumer) accept(ctx context.Context, delivery CrawlOrderDelivery) {
	order := delivery.Order
	if err := c.registry.Register(order.Profile); err != nil {
		slog.Warn(msgProfileRegisterFailed, "handle", order.Profile.Handle, "error", err)
		if err := delivery.Term(ctx); err != nil {
			slog.Warn(msgOrderTermFailed, "handle", order.Profile.Handle, "error", err)
		}
		return
	}
	c.frontier.Hold()
	c.frontier.SeedRun(ctx, order.Requests, order.Provenance, func() {
		defer c.frontier.Release()
		if ctx.Err() != nil {
			if err := delivery.Nak(context.Background()); err != nil {
				slog.Warn(msgOrderNakFailed, "handle", order.Profile.Handle, "error", err)
			}
			return
		}
		if err := delivery.Ack(context.Background()); err != nil {
			slog.Warn(msgOrderAckFailed, "handle", order.Profile.Handle, "error", err)
		}
	})
}
