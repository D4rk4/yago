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
	discarded := false
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
		alreadyLeased, err := q.discardPendingAutomaticDiscoveryLeaseDuplicateTx(
			tx,
			selected,
		)
		if err != nil {
			return err
		}
		if alreadyLeased {
			found = false
			discarded = true

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
	if discarded {
		q.signal()
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
	discoveryKey, discovered, err := q.pendingDiscoveryKeys.Get(tx, lease.order.key)
	if err != nil {
		return fmt.Errorf("read pending crawl discovery: %w", err)
	}
	sequence, err := (sequenceCodec{}).Decode(lease.order.key)
	if err != nil {
		return fmt.Errorf("decode pending crawl discovery sequence: %w", err)
	}
	if !discovered {
		var key string
		key, discovered, err = q.activeAutomaticDiscoveryForOrderTx(
			tx,
			lease.order.data,
			sequence,
		)
		discoveryKey = []byte(key)
		if err != nil {
			return err
		}
	}
	if discovered {
		record.DiscoveryKey = string(discoveryKey)
		record.DiscoverySequence = sequence
		if _, err := q.pendingDiscoveryKeys.Delete(tx, lease.order.key); err != nil {
			return fmt.Errorf("delete pending crawl discovery: %w", err)
		}
	}
	if err := q.leases.Put(tx, vault.Key(lease.leaseID), record); err != nil {
		return fmt.Errorf("store crawl lease: %w", err)
	}
	if discovered {
		if err := q.recordLeasedAutomaticDiscoveryTx(
			tx,
			string(discoveryKey),
			vault.Key(lease.leaseID),
		); err != nil {
			return err
		}
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
