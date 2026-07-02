package crawlorder

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/boundedqueue"
	"github.com/D4rk4/yago/yacycrawler/internal/crawladmission"
	"github.com/D4rk4/yago/yacycrawler/internal/frontier"
)

const (
	msgOrderReceived         = "crawl order received"
	msgRunSeeded             = "crawl run seeded"
	msgProfileRegisterFailed = "crawl profile registration failed"
	msgOrderExpansionFailed  = "crawl order expansion failed"
	msgOrderAckFailed        = "crawl order ack failed"
	msgOrderNakFailed        = "crawl order nak failed"
	msgOrderTermFailed       = "crawl order term failed"
)

type RequestExpander interface {
	Expand(
		ctx context.Context,
		requests []yacycrawlcontract.CrawlRequest,
	) ([]yacycrawlcontract.CrawlRequest, error)
}

type CrawlOrderConsumer struct {
	orders   boundedqueue.Receiver[CrawlOrderDelivery]
	frontier *frontier.Frontier
	expander RequestExpander
}

func NewCrawlOrderConsumer(
	orders boundedqueue.Receiver[CrawlOrderDelivery],
	frontier *frontier.Frontier,
	expander ...RequestExpander,
) *CrawlOrderConsumer {
	selected := RequestExpander(passThroughRequestExpander{})
	if len(expander) > 0 && expander[0] != nil {
		selected = expander[0]
	}
	return &CrawlOrderConsumer{orders: orders, frontier: frontier, expander: selected}
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
	profile, err := crawladmission.CompileProfile(order.Profile)
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
	requests, err := c.expander.Expand(ctx, order.Requests)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgOrderExpansionFailed,
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
	seeded := c.frontier.SeedRun(ctx, requests, order.Provenance, profile, func() {
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

type passThroughRequestExpander struct{}

func (passThroughRequestExpander) Expand(
	_ context.Context,
	requests []yacycrawlcontract.CrawlRequest,
) ([]yacycrawlcontract.CrawlRequest, error) {
	out := make([]yacycrawlcontract.CrawlRequest, 0, len(requests))
	for _, request := range requests {
		mode, ok := yacycrawlcontract.NormalizeCrawlRequestMode(request.Mode)
		if !ok {
			return nil, fmt.Errorf("unsupported crawl request mode %q", request.Mode)
		}
		request.Mode = mode
		out = append(out, request)
	}
	return out, nil
}
