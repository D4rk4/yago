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
		available, err := q.workerLeaseCapacityAvailable(
			tx,
			workerID,
			workerSessionID,
			leasedAt,
		)
		if err != nil {
			return err
		}
		if !available {
			found = false

			return nil
		}

		return q.persistPendingOrderLeaseTx(tx, pendingOrderLease{
			order:           selected,
			leaseID:         leaseID,
			workerID:        workerID,
			workerSessionID: workerSessionID,
			leasedAt:        leasedAt,
			nextBurst:       nextBurst,
			updateBurst:     updateBurst,
		})
	})
	if err != nil {
		return pendingOrderHead{}, false, fmt.Errorf("claim pending crawl order: %w", err)
	}

	return selected, found, nil
}

func (q *DurableOrderQueue) workerLeaseCapacityAvailable(
	tx *vault.Txn,
	workerID string,
	workerSessionID string,
	leasedAt time.Time,
) (bool, error) {
	if workerSessionID == "" {
		return true, nil
	}
	capacityReached, err := q.workerLeaseCapacityReached(tx, workerID, workerSessionID, leasedAt)
	if err != nil {
		return false, fmt.Errorf("count active worker crawl leases: %w", err)
	}

	return !capacityReached, nil
}

func (q *DurableOrderQueue) persistPendingOrderLeaseTx(
	tx *vault.Txn,
	lease pendingOrderLease,
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
	record := leaseRecord{
		OrderData:         lease.order.data,
		Priority:          orderPriority(lease.order.automatic),
		WorkerID:          lease.workerID,
		WorkerSessionID:   lease.workerSessionID,
		ExpiresAtUnixNano: lease.leasedAt.Add(q.leaseTTL).UnixNano(),
	}
	if err := q.leases.Put(tx, vault.Key(lease.leaseID), record); err != nil {
		return fmt.Errorf("store crawl lease: %w", err)
	}

	return nil
}
