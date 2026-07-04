package crawlorder

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawladmission"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/frontier"
)

const (
	msgOrderReceived         = "crawl order received"
	msgRunSeeded             = "crawl run seeded"
	msgProfileRegisterFailed = "crawl profile registration failed"
	msgOrderIDInvalid        = "crawl order id is not a valid identifier"
	msgOrderDuplicate        = "crawl order already active, naking redelivery"
	msgOrderAckFailed        = "crawl order ack failed"
	msgOrderNakFailed        = "crawl order nak failed"
	msgOrderTermFailed       = "crawl order term failed"
)

type CrawlOrderIntake interface {
	Receive() <-chan CrawlOrderDelivery
}

type CrawlOrderConsumer struct {
	orders     CrawlOrderIntake
	frontier   *frontier.Frontier
	redelivery OrderRedeliveryPolicy
}

func NewCrawlOrderConsumer(
	orders CrawlOrderIntake,
	frontier *frontier.Frontier,
	redelivery OrderRedeliveryPolicy,
) *CrawlOrderConsumer {
	return &CrawlOrderConsumer{
		orders:     orders,
		frontier:   frontier,
		redelivery: redelivery,
	}
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
	slog.InfoContext(
		ctx,
		msgOrderReceived,
		slog.String("handle", order.Profile.Handle),
		slog.Int("seeds", len(order.Requests)),
	)
	runID, err := uuid.Parse(order.OrderID)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgOrderIDInvalid,
			slog.String("orderId", order.OrderID),
			slog.Any("error", err),
		)
		c.term(ctx, delivery)
		return
	}
	profile, err := crawladmission.CompileProfile(order.Profile)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgProfileRegisterFailed,
			slog.String("handle", order.Profile.Handle),
			slog.Any("error", err),
		)
		c.term(ctx, delivery)
		return
	}
	c.frontier.Hold()
	heartbeat := keepOrderAlive(ctx, delivery, c.redelivery.heartbeatInterval())
	queued, duplicate := c.frontier.SeedRun(ctx, frontier.RunSeeds{
		RunID:      runID,
		Requests:   order.Requests,
		Provenance: order.Provenance,
		Profile:    profile,
	}, c.settleRun(ctx, delivery, heartbeat))
	if duplicate {
		heartbeat.release()
		c.frontier.Release()
		slog.WarnContext(
			ctx,
			msgOrderDuplicate,
			slog.String("handle", order.Profile.Handle),
			slog.String("runId", runID.String()),
		)
		c.nak(ctx, delivery)
		return
	}
	slog.InfoContext(
		ctx,
		msgRunSeeded,
		slog.String("handle", order.Profile.Handle),
		slog.String("runId", runID.String()),
		slog.Int("queued", queued),
	)
}

func (c *CrawlOrderConsumer) settleRun(
	ctx context.Context,
	delivery CrawlOrderDelivery,
	heartbeat *orderHeartbeat,
) func(succeeded bool) {
	return func(succeeded bool) {
		heartbeat.release()
		defer c.frontier.Release()
		if ctx.Err() != nil || !succeeded {
			c.nak(context.Background(), delivery)
			return
		}
		if err := delivery.Ack(context.Background()); err != nil {
			slog.WarnContext(
				context.Background(),
				msgOrderAckFailed,
				slog.String("handle", delivery.Order.Profile.Handle),
				slog.Any("error", err),
			)
		}
	}
}

func (c *CrawlOrderConsumer) nak(ctx context.Context, delivery CrawlOrderDelivery) {
	if err := delivery.Nak(ctx); err != nil {
		slog.WarnContext(
			ctx,
			msgOrderNakFailed,
			slog.String("handle", delivery.Order.Profile.Handle),
			slog.Any("error", err),
		)
	}
}

func (c *CrawlOrderConsumer) term(ctx context.Context, delivery CrawlOrderDelivery) {
	if err := delivery.Term(ctx); err != nil {
		slog.WarnContext(
			ctx,
			msgOrderTermFailed,
			slog.String("handle", delivery.Order.Profile.Handle),
			slog.Any("error", err),
		)
	}
}
