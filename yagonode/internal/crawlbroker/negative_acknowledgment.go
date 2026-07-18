package crawlbroker

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const negativeAcknowledgmentRetryDelay = 5 * time.Second

func (q *DurableOrderQueue) deferLease(ctx context.Context, leaseID string) error {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	err := q.deferLeaseLocked(ctx, leaseID, "", "", false)
	if err == nil {
		q.signal()
	}

	return err
}

func (q *DurableOrderQueue) deferLeaseForOwner(
	ctx context.Context,
	leaseID string,
	workerID string,
	workerSessionID string,
) error {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	err := q.deferLeaseLocked(ctx, leaseID, workerID, workerSessionID, true)
	if err == nil {
		q.signal()
	}

	return err
}

func (q *DurableOrderQueue) deferLeaseLocked(
	ctx context.Context,
	leaseID string,
	workerID string,
	workerSessionID string,
	requireOwner bool,
) error {
	var removed leaseRecord
	removedFound := false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		record, ok, err := q.leases.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read crawl lease: %w", err)
		}
		if !ok {
			return errLeaseDispositionConflict
		}
		if record.WorkerID == "" {
			if record.Deferred {
				return nil
			}

			return errLeaseDispositionConflict
		}
		removed = record
		removedFound = true
		if requireOwner && !liveLeaseOwnedBy(
			record,
			workerID,
			workerSessionID,
			nowFunc(),
		) {
			return errLeaseLost
		}
		if err := q.recordLeaseSettlement(tx, leaseID, leaseSettlementRequeued); err != nil {
			return err
		}
		record.WorkerID = ""
		record.Deferred = true
		record.ExpiresAtUnixNano = nowFunc().Add(negativeAcknowledgmentRetryDelay).UnixNano()
		if err := q.leases.Put(tx, vault.Key(leaseID), record); err != nil {
			return fmt.Errorf("defer crawl lease: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("nak crawl lease: %w", err)
	}
	if removedFound {
		q.workerLeases.remove(removed)
	}

	return nil
}

func leaseSweepInterval(leaseTTL time.Duration) time.Duration {
	return max(min(leaseTTL/4, negativeAcknowledgmentRetryDelay/2), time.Second)
}
