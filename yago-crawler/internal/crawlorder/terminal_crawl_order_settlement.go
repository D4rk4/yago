package crawlorder

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type terminalCrawlOrderDisposition struct {
	order          yagocrawlcontract.CrawlOrder
	delivery       CrawlOrderDelivery
	disposition    crawlOrderDisposition
	state          yagocrawlcontract.CrawlRunState
	tally          yagocrawlcontract.CrawlRunTally
	recentOutcomes yagocrawlcontract.CrawlURLOutcomeHistory
	pagesPerMinute uint32
	retryAfter     time.Duration
}

func settleTerminalCrawlOrder(
	ctx context.Context,
	settlement terminalCrawlOrderDisposition,
) bool {
	if settlement.disposition == crawlOrderRetained {
		return true
	}
	if settlement.disposition == crawlOrderRequeued && settlement.retryAfter > 0 &&
		settlement.delivery.NakAfter != nil {
		return settleCrawlOrder(
			ctx,
			settlement.order,
			settlement.delivery,
			settlement.disposition,
			settlement.retryAfter,
		)
	}
	if settlement.delivery.settleTerminal == nil {
		return settleCrawlOrder(
			ctx,
			settlement.order,
			settlement.delivery,
			settlement.disposition,
			settlement.retryAfter,
		)
	}
	err := settlement.delivery.settleTerminal(ctx, terminalRunSettlement{
		Disposition:    settlement.disposition,
		State:          settlement.state,
		Tally:          settlement.tally,
		RecentOutcomes: settlement.recentOutcomes,
		PagesPerMinute: settlement.pagesPerMinute,
		RetryAfter:     settlement.retryAfter,
		RateKnown:      true,
	})
	if err == nil {
		return true
	}
	logCrawlOrderSettlementFailure(
		ctx,
		settlement.disposition,
		settlement.order.Profile.Handle,
		err,
	)

	return false
}
