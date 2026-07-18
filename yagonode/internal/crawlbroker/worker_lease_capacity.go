package crawlbroker

import "github.com/D4rk4/yago/yagocrawlcontract"

func (q *DurableOrderQueue) workerLeaseCapacityReached(
	workerID string,
	workerSessionID string,
) bool {
	return q.workerLeases.reached(
		workerID,
		workerSessionID,
		yagocrawlcontract.MaximumHeartbeatActiveLeases,
	)
}
