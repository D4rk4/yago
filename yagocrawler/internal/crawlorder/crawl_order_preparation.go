package crawlorder

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawladmission"
)

func (c *CrawlOrderConsumer) prepareCrawlOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
) (crawladmission.AdmissionProfile, []yagocrawlcontract.CrawlRequest, bool) {
	if err := validateCrawlOrderRequests(order.Requests); err != nil {
		slog.WarnContext(
			ctx,
			msgOrderValidationFailed,
			slog.String("handle", order.Profile.Handle),
			slog.Any("error", err),
		)
		c.terminateOrder(ctx, order, delivery)

		return crawladmission.AdmissionProfile{}, nil, false
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

		return crawladmission.AdmissionProfile{}, nil, false
	}
	requests, err := c.expander.Expand(ctx, order.Requests)
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
