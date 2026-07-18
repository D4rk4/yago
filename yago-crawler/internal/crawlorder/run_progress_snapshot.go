package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (c *CrawlOrderConsumer) reportRun(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	state yagocrawlcontract.CrawlRunState,
	pending int,
	leaseIDs ...string,
) {
	tally := c.runTally(order.Provenance, pending)
	c.reportRunTally(ctx, order, state, tally, leaseIDs...)
}

func (c *CrawlOrderConsumer) runTally(
	provenance []byte,
	pending int,
) yagocrawlcontract.CrawlRunTally {
	var tally yagocrawlcontract.CrawlRunTally
	if c.tally != nil {
		tally = c.tally.Snapshot(provenance)
	}
	tally.Pending = 0
	if pending > 0 {
		tally.Pending = uint64(pending)
	}

	return tally
}
