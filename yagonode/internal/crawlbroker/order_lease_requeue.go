package crawlbroker

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) matchingLeaseKeys(
	ctx context.Context,
	match func(leaseRecord) bool,
) ([]vault.Key, error) {
	keys := make([]vault.Key, 0)
	err := q.vault.View(ctx, func(tx *vault.Txn) error {
		return q.leases.Scan(tx, nil, func(
			key vault.Key,
			record leaseRecord,
		) (bool, error) {
			if match(record) {
				keys = append(keys, append(vault.Key(nil), key...))
			}

			return true, nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("requeue crawl leases: scan crawl leases: %w", err)
	}

	return keys, nil
}

func (q *DurableOrderQueue) requeueLeaseChunk(
	ctx context.Context,
	keys []vault.Key,
	match func(leaseRecord) bool,
) (bool, error) {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	requeued := false
	removed := make([]leaseRecord, 0, len(keys))
	err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		requeued = false
		removed = removed[:0]
		for _, key := range keys {
			record, changed, err := q.requeueLeaseTx(tx, key, match)
			if err != nil {
				return err
			}
			if changed {
				removed = append(removed, record)
			}
			requeued = requeued || changed
		}

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("commit crawl lease requeue: %w", err)
	}
	for _, record := range removed {
		q.workerLeases.remove(record)
	}

	return requeued, nil
}

func (q *DurableOrderQueue) requeueLeaseTx(
	tx *vault.Txn,
	key vault.Key,
	match func(leaseRecord) bool,
) (leaseRecord, bool, error) {
	record, found, err := q.leases.Get(tx, key)
	if err != nil {
		return leaseRecord{}, false, fmt.Errorf("read crawl lease: %w", err)
	}
	if !found || !match(record) {
		return leaseRecord{}, false, nil
	}
	if _, err := q.leases.Delete(tx, key); err != nil {
		return leaseRecord{}, false, fmt.Errorf("delete crawl lease: %w", err)
	}
	if _, err := q.enqueueTx(
		tx,
		record.OrderData,
		persistedOrderPriority(record.OrderData, record.Priority),
	); err != nil {
		return leaseRecord{}, false, err
	}
	if err := q.recordLeaseSettlement(tx, string(key), leaseSettlementRequeued); err != nil {
		return leaseRecord{}, false, err
	}

	return record, true, nil
}
