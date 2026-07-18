package crawlorder

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type crawlOrderDisposition uint8

const (
	crawlOrderAcknowledged crawlOrderDisposition = iota
	crawlOrderRequeued
	crawlOrderTerminated
	crawlOrderRetained
)

func settleCrawlOrder(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	delivery CrawlOrderDelivery,
	disposition crawlOrderDisposition,
) bool {
	var err error
	switch disposition {
	case crawlOrderRetained:
		return true
	case crawlOrderRequeued:
		err = delivery.Nak(ctx)
	case crawlOrderTerminated:
		err = delivery.Term(ctx)
	default:
		err = delivery.Ack(ctx)
	}
	if err == nil {
		return true
	}
	logCrawlOrderSettlementFailure(ctx, disposition, order.Profile.Handle, err)

	return false
}

func logCrawlOrderSettlementFailure(
	ctx context.Context,
	disposition crawlOrderDisposition,
	handle string,
	err error,
) {
	attributes := []any{slog.String("handle", handle), slog.Any("error", err)}
	switch disposition {
	case crawlOrderRequeued:
		slog.WarnContext(ctx, msgOrderNakFailed, attributes...)
	case crawlOrderTerminated:
		slog.WarnContext(ctx, msgOrderTermFailed, attributes...)
	default:
		slog.WarnContext(ctx, msgOrderAckFailed, attributes...)
	}
}
