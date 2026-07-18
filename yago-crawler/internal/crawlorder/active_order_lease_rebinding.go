package crawlorder

import "github.com/D4rk4/yago/yago-crawler/internal/frontier"

type activeOrderLeaseRebinder func(string, string) activeOrderClaim

func (c *CrawlOrderConsumer) leaseRebinder(provenance []byte) activeOrderLeaseRebinder {
	return func(previousLeaseID string, replacementLeaseID string) activeOrderClaim {
		switch c.frontier.RebindRunLease(provenance, previousLeaseID, replacementLeaseID) {
		case frontier.RunLeaseRebound:
			return activeOrderJoinsRun
		case frontier.RunLeaseAlreadyComplete:
			return activeOrderRecoversCompletedRun
		default:
			return activeOrderRejected
		}
	}
}
