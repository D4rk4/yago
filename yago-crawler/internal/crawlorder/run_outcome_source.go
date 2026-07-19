package crawlorder

import "github.com/D4rk4/yago/yagocrawlcontract"

type runOutcomeSource interface {
	RecentOutcomes([]byte) yagocrawlcontract.CrawlURLOutcomeHistory
}

func (c *CrawlOrderConsumer) recentRunOutcomes(
	provenance []byte,
) yagocrawlcontract.CrawlURLOutcomeHistory {
	source, ok := c.tally.(runOutcomeSource)
	if !ok {
		return yagocrawlcontract.CrawlURLOutcomeHistory{}
	}

	return source.RecentOutcomes(provenance)
}
