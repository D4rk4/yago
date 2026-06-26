package crawlorder

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/boundedqueue"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlscope"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawlwork"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/frontier"
)

const (
	msgOrderReceived         = "crawl order received"
	msgRunSeeded             = "crawl run seeded"
	msgProfileRegisterFailed = "crawl profile registration failed"
	msgOrderAckFailed        = "crawl order ack failed"
	msgOrderNakFailed        = "crawl order nak failed"
	msgOrderTermFailed       = "crawl order term failed"
)

type CrawlOrderConsumer struct {
	orders   boundedqueue.Receiver[crawlwork.CrawlOrderDelivery]
	frontier *frontier.Frontier
}

func NewCrawlOrderConsumer(
	orders boundedqueue.Receiver[crawlwork.CrawlOrderDelivery],
	frontier *frontier.Frontier,
) *CrawlOrderConsumer {
	return &CrawlOrderConsumer{orders: orders, frontier: frontier}
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

func (c *CrawlOrderConsumer) accept(ctx context.Context, delivery crawlwork.CrawlOrderDelivery) {
	order := delivery.Order
	slog.InfoContext(
		ctx,
		msgOrderReceived,
		slog.String("handle", order.Profile.Handle),
		slog.Int("seeds", len(order.Requests)),
	)
	profile, err := crawlscope.CompileProfile(order.Profile)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgProfileRegisterFailed,
			slog.String("handle", order.Profile.Handle),
			slog.Any("error", err),
		)
		if err := delivery.Term(ctx); err != nil {
			slog.WarnContext(
				ctx,
				msgOrderTermFailed,
				slog.String("handle", order.Profile.Handle),
				slog.Any("error", err),
			)
		}
		return
	}
	c.frontier.Hold()
	seeded := c.frontier.SeedRun(ctx, order.Requests, order.Provenance, profile, func() {
		defer c.frontier.Release()
		if ctx.Err() != nil {
			if err := delivery.Nak(context.Background()); err != nil {
				slog.WarnContext(
					context.Background(),
					msgOrderNakFailed,
					slog.String("handle", order.Profile.Handle),
					slog.Any("error", err),
				)
			}
			return
		}
		if err := delivery.Ack(context.Background()); err != nil {
			slog.WarnContext(
				context.Background(),
				msgOrderAckFailed,
				slog.String("handle", order.Profile.Handle),
				slog.Any("error", err),
			)
		}
	})
	slog.InfoContext(
		ctx,
		msgRunSeeded,
		slog.String("handle", order.Profile.Handle),
		slog.String("runId", seeded.RunID.String()),
		slog.Int("queued", seeded.Queued),
	)
}
