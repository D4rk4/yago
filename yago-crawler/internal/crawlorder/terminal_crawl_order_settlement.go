package crawlorder

import (
	"context"

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
}

func settleTerminalCrawlOrder(
	ctx context.Context,
	settlement terminalCrawlOrderDisposition,
) bool {
	if settlement.disposition == crawlOrderRetained {
		return true
	}
	if settlement.delivery.settleTerminal == nil {
		return settleCrawlOrder(
			ctx,
			settlement.order,
			settlement.delivery,
			settlement.disposition,
		)
	}
	err := settlement.delivery.settleTerminal(ctx, terminalRunSettlement{
		Disposition:    settlement.disposition,
		State:          settlement.state,
		Tally:          settlement.tally,
		RecentOutcomes: settlement.recentOutcomes,
		PagesPerMinute: settlement.pagesPerMinute,
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
