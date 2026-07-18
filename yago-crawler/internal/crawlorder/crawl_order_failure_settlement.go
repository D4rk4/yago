package crawlorder

import (
	"context"
	"errors"

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
	settled := c.active.settle(
		order.Provenance,
		delivery,
		true,
		func(delivery CrawlOrderDelivery) bool {
			return settleCrawlOrder(
				context.WithoutCancel(ctx),
				order,
				delivery,
				crawlOrderTerminated,
			)
		},
	)
	if settled && c.frontier.CheckpointFailure() == nil {
		c.forgetCheckpoint(context.WithoutCancel(ctx), order)
	}
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
		func(delivery CrawlOrderDelivery) bool {
			return settleCrawlOrder(
				context.WithoutCancel(ctx),
				order,
				delivery,
				crawlOrderRequeued,
			)
		},
	)
}

func (c *CrawlOrderConsumer) retainOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
) {
	c.active.settle(
		order.Provenance,
		delivery,
		false,
		func(delivery CrawlOrderDelivery) bool {
			return settleCrawlOrder(
				context.WithoutCancel(ctx),
				order,
				delivery,
				crawlOrderRetained,
			)
		},
	)
}
