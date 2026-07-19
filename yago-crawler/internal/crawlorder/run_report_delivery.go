package crawlorder

import (
	"context"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (c *CrawlOrderConsumer) reportRunTally(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	state yagocrawlcontract.CrawlRunState,
	tally yagocrawlcontract.CrawlRunTally,
	leaseIDs ...string,
) {
	if c.progress == nil {
		return
	}
	leaseID := ""
	if len(leaseIDs) > 0 {
		leaseID = leaseIDs[0]
	}
	c.progress.ReportRun(ctx, RunReport{
		Provenance:      order.Provenance,
		LeaseID:         leaseID,
		ProfileHandle:   order.Profile.Handle,
		ProfileName:     order.Profile.Name,
		State:           state,
		Tally:           tally,
		RecentOutcomes:  c.recentRunOutcomes(order.Provenance),
		PagesPerMinute:  c.frontier.EffectivePagesPerMinute(order.Provenance),
		MaxPagesPerHost: order.Profile.MaxPagesPerHost,
		MaxPagesPerRun: order.EffectiveMaxPagesPerRun(
			yagocrawlcontract.DefaultMaxPagesPerRun,
		),
	})
}
