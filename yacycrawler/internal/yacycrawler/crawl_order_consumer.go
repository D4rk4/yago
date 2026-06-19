package yacycrawler

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

const msgProfileRegisterFailed = "crawl profile registration failed"

type CrawlOrderConsumer struct {
	orders   Receiver[yacycrawlcontract.CrawlOrder]
	registry *CrawlProfileRegistry
	frontier *Frontier
}

func NewCrawlOrderConsumer(
	orders Receiver[yacycrawlcontract.CrawlOrder],
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
		case order, ok := <-c.orders.Receive():
			if !ok {
				return
			}
			c.accept(ctx, order)
		}
	}
}

func (c *CrawlOrderConsumer) accept(ctx context.Context, order yacycrawlcontract.CrawlOrder) {
	if err := c.registry.Register(order.Profile); err != nil {
		slog.Warn(msgProfileRegisterFailed, "handle", order.Profile.Handle, "error", err)
		return
	}
	c.frontier.Hold()
	c.frontier.SeedRun(ctx, order.Requests, order.Provenance, c.frontier.Release)
}
