package crawlorder

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (c *CrawlOrderConsumer) prepareCrawlOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
) (crawladmission.AdmissionProfile, []yagocrawlcontract.CrawlRequest, bool) {
	profile, prepared := c.compileCrawlOrder(ctx, order, delivery)
	if !prepared {
		return crawladmission.AdmissionProfile{}, nil, false
	}
	requests, err := c.expander.Expand(ctx, order.Requests)
	if ctx.Err() != nil {
		c.retainOrder(ctx, order, delivery)

		return crawladmission.AdmissionProfile{}, nil, false
	}
	if err != nil {
		slog.WarnContext(
			ctx,
			msgOrderExpansionFailed,
			slog.String("handle", order.Profile.Handle),
			slog.Any("error", err),
		)
		if expansionFailureIsPermanent(err) {
			c.terminateOrder(ctx, order, delivery)
		} else {
			c.requeueOrder(ctx, order, delivery)
		}

		return crawladmission.AdmissionProfile{}, nil, false
	}

	return profile, requests, true
}

func (c *CrawlOrderConsumer) compileCrawlOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
) (crawladmission.AdmissionProfile, bool) {
	if err := validateCrawlOrderRequests(order.Requests); err != nil {
		slog.WarnContext(
			ctx,
			msgOrderValidationFailed,
			slog.String("handle", order.Profile.Handle),
			slog.Any("error", err),
		)
		c.terminateOrder(ctx, order, delivery)

		return crawladmission.AdmissionProfile{}, false
	}
	profile, err := crawladmission.CompileProfile(order.Profile)
	if err != nil {
		slog.WarnContext(
			ctx,
			msgProfileRegisterFailed,
			slog.String("handle", order.Profile.Handle),
			slog.Any("error", err),
		)
		c.terminateOrder(ctx, order, delivery)

		return crawladmission.AdmissionProfile{}, false
	}

	return profile, true
}
