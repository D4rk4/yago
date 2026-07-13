package crawlorder

import (
	"context"
	"errors"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type permanentExpansionFailure interface {
	error
	Permanent() bool
}

func expansionFailureIsPermanent(err error) bool {
	var permanent permanentExpansionFailure

	return errors.As(err, &permanent) && permanent.Permanent()
}

func (c *CrawlOrderConsumer) terminateOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
) {
	c.active.settle(
		order.Provenance,
		delivery,
		true,
		func(delivery CrawlOrderDelivery) {
			settlementCtx := context.WithoutCancel(ctx)
			if err := delivery.Term(settlementCtx); err != nil {
				slog.WarnContext(
					settlementCtx,
					msgOrderTermFailed,
					slog.String("handle", order.Profile.Handle),
					slog.Any("error", err),
				)
			}
		},
	)
}

func (c *CrawlOrderConsumer) requeueOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
) {
	c.active.settle(
		order.Provenance,
		delivery,
		false,
		func(delivery CrawlOrderDelivery) {
			settlementCtx := context.WithoutCancel(ctx)
			if err := delivery.Nak(settlementCtx); err != nil {
				slog.WarnContext(
					settlementCtx,
					msgOrderNakFailed,
					slog.String("handle", order.Profile.Handle),
					slog.Any("error", err),
				)
			}
		},
	)
}
