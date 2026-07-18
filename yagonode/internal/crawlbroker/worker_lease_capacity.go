package crawlbroker

import (
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) workerLeaseCapacityReached(
	tx *vault.Txn,
	workerID string,
	workerSessionID string,
	at time.Time,
) (bool, error) {
	active := 0
	err := q.leases.Scan(tx, nil, func(
		_ vault.Key,
		record leaseRecord,
	) (bool, error) {
		if liveLeaseOwnedBy(record, workerID, workerSessionID, at) {
			active++
		}

		return active < yagocrawlcontract.MaximumHeartbeatActiveLeases, nil
	})
	if err != nil {
		return false, fmt.Errorf("scan active worker crawl leases: %w", err)
	}

	return active >= yagocrawlcontract.MaximumHeartbeatActiveLeases, nil
}
