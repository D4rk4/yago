package crawlbroker

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type pendingOrderLease struct {
	order           pendingOrderHead
	leaseID         string
	workerID        string
	workerSessionID string
	leasedAt        time.Time
	nextBurst       uint64
	updateBurst     bool
}

func (q *DurableOrderQueue) claimPendingOrder(
	ctx context.Context,
	leaseID string,
	workerID string,
	workerSessionID string,
) (pendingOrderHead, bool, error) {
	var selected pendingOrderHead
	var claimed leaseRecord
	found := false
	err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var nextBurst uint64
		var updateBurst bool
		var err error
		selected, nextBurst, updateBurst, err = q.selectPendingOrder(tx)
		if err != nil {
			return err
		}
		found = selected.found
		if !found {
			return nil
		}
		leasedAt := nowFunc()
		lease := pendingOrderLease{
			order:           selected,
			leaseID:         leaseID,
			workerID:        workerID,
			workerSessionID: workerSessionID,
			leasedAt:        leasedAt,
			nextBurst:       nextBurst,
			updateBurst:     updateBurst,
		}
		claimed = lease.record(q.leaseTTL)

		return q.persistPendingOrderLeaseTx(tx, lease, claimed)
	})
	if err != nil {
		return pendingOrderHead{}, false, fmt.Errorf("claim pending crawl order: %w", err)
	}
	if found {
		q.workerLeases.add(claimed)
	}

	return selected, found, nil
}

func (q *DurableOrderQueue) workerLeaseCapacityAvailable(
	workerID string,
	workerSessionID string,
) bool {
	if workerSessionID == "" {
		return true
	}

	return !q.workerLeaseCapacityReached(workerID, workerSessionID)
}

func (q *DurableOrderQueue) persistPendingOrderLeaseTx(
	tx *vault.Txn,
	lease pendingOrderLease,
	record leaseRecord,
) error {
	if lease.updateBurst {
		if err := q.seq.Put(tx, priorityBurstKey, lease.nextBurst); err != nil {
			return fmt.Errorf("store automatic crawl burst: %w", err)
		}
	}
	if _, err := q.orders.Delete(tx, lease.order.key); err != nil {
		return fmt.Errorf("delete crawl order: %w", err)
	}
	if _, err := lease.order.index.Delete(tx, lease.order.key); err != nil {
		return fmt.Errorf("delete crawl order priority: %w", err)
	}
	if err := q.leases.Put(tx, vault.Key(lease.leaseID), record); err != nil {
		return fmt.Errorf("store crawl lease: %w", err)
	}

	return nil
}

func (lease pendingOrderLease) record(leaseTTL time.Duration) leaseRecord {
	return leaseRecord{
		OrderData:         lease.order.data,
		Priority:          orderPriority(lease.order.automatic),
		WorkerID:          lease.workerID,
		WorkerSessionID:   lease.workerSessionID,
		ExpiresAtUnixNano: lease.leasedAt.Add(leaseTTL).UnixNano(),
	}
}
