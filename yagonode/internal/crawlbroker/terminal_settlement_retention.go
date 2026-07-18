package crawlbroker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) finalizeTerminalLeaseSettlement(
	ctx context.Context,
	leaseID string,
	definition leaseSettlementRecord,
) error {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		record, err := q.requireTerminalLeaseSettlement(tx, leaseID, definition)
		if err != nil {
			return err
		}
		if record.FinalizedAtUnixNano != 0 {
			return nil
		}
		if !record.ProgressDelivered {
			return errLeaseDispositionConflict
		}
		if record.Outcome == leaseSettlementAcknowledged {
			if err := q.requireCompletedTerminalControlTx(tx, leaseID, record); err != nil {
				return err
			}
		}
		record.FinalizedAtUnixNano = nowFunc().UnixNano()
		if record.FinalizedAtUnixNano <= 0 {
			return fmt.Errorf("invalid terminal crawl settlement finalization time")
		}
		if err := q.leaseSettlements.Put(tx, vault.Key(leaseID), record); err != nil {
			return fmt.Errorf("finalize terminal crawl settlement: %w", err)
		}
		if err := q.leaseSettlementExpiry.Put(
			tx,
			leaseSettlementExpiryKey(record),
			[]byte(leaseID),
		); err != nil {
			return fmt.Errorf("store terminal crawl settlement expiry: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("commit terminal crawl settlement finalization: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) requireCompletedTerminalControlTx(
	tx *vault.Txn,
	leaseID string,
	settlement leaseSettlementRecord,
) error {
	_, pending, err := q.leaseControlTargets.Get(tx, vault.Key(leaseID))
	if err != nil {
		return fmt.Errorf("read pending terminal crawl control target: %w", err)
	}
	if pending {
		return errLeaseDispositionConflict
	}
	completed, found, err := q.completedControlTargets.Get(tx, vault.Key(leaseID))
	if err != nil {
		return fmt.Errorf("read completed terminal crawl control target: %w", err)
	}
	if !found || completed.WorkerID != settlement.Progress.WorkerID ||
		completed.RunID != settlement.Progress.RunID {
		return errLeaseDispositionConflict
	}

	return nil
}

func (q *DurableOrderQueue) expireTerminalLeaseSettlementTx(
	tx *vault.Txn,
	candidate expiredLeaseSettlementIndex,
	settlement leaseSettlementRecord,
) (bool, error) {
	if err := q.verifyExpiredLeaseSettlementIndex(
		tx,
		candidate.leaseID,
		settlement.Sequence,
	); err != nil {
		return false, err
	}
	requeued := false
	if settlement.Outcome == leaseSettlementRequeued {
		var err error
		requeued, err = q.requeueTerminalLeaseTx(
			tx,
			candidate.leaseID,
			settlement.OrderIdentity,
		)
		if err != nil {
			return false, err
		}
	}
	if err := q.deleteTerminalLeaseSettlementTx(
		tx,
		candidate.leaseID,
		settlement,
	); err != nil {
		return false, err
	}

	return requeued, nil
}

func (q *DurableOrderQueue) requeueTerminalLeaseTx(
	tx *vault.Txn,
	leaseID string,
	orderIdentity []byte,
) (bool, error) {
	lease, found, err := q.leases.Get(tx, vault.Key(leaseID))
	if err != nil {
		return false, fmt.Errorf("read deferred crawl lease: %w", err)
	}
	if !found {
		return false, nil
	}
	identity := sha256.Sum256(lease.OrderData)
	if !lease.Deferred || lease.WorkerID != "" || !bytes.Equal(identity[:], orderIdentity) {
		return false, errLeaseDispositionConflict
	}
	if _, err := q.enqueueTx(
		tx,
		lease.OrderData,
		persistedOrderPriority(lease.OrderData, lease.Priority),
	); err != nil {
		return false, err
	}
	if _, err := q.leases.Delete(tx, vault.Key(leaseID)); err != nil {
		return false, fmt.Errorf("delete deferred crawl lease: %w", err)
	}

	return true, nil
}
