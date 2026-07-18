package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type leasedCrawlOrder struct {
	LeaseID   string
	OrderData []byte
}

func (q *DurableOrderQueue) leasedOrdersForWorker(
	ctx context.Context,
	workerID string,
) ([]leasedCrawlOrder, error) {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	var leasedOrders []leasedCrawlOrder
	now := nowFunc()
	deadline := now.Add(q.leaseTTL).UnixNano()
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		leasedOrders = nil
		var keys []vault.Key
		var records []leaseRecord
		if err := q.leases.Scan(tx, nil, func(key vault.Key, record leaseRecord) (bool, error) {
			if record.WorkerID != workerID || record.ExpiresAtUnixNano <= now.UnixNano() {
				return true, nil
			}
			record.ExpiresAtUnixNano = deadline
			keys = append(keys, key)
			records = append(records, record)
			leasedOrders = append(leasedOrders, leasedCrawlOrder{
				LeaseID:   string(key),
				OrderData: append([]byte(nil), record.OrderData...),
			})

			return true, nil
		}); err != nil {
			return fmt.Errorf("scan worker crawl leases: %w", err)
		}
		for i, key := range keys {
			if err := q.leases.Put(tx, key, records[i]); err != nil {
				return fmt.Errorf("renew worker crawl lease: %w", err)
			}
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("read worker crawl leases: %w", err)
	}
	q.mu.Lock()
	if len(leasedOrders) == 0 {
		delete(q.extendedAt, workerID)
	} else {
		q.extendedAt[workerID] = now
	}
	q.mu.Unlock()

	return leasedOrders, nil
}
