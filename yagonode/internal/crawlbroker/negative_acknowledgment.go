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
	var mutation negativeAcknowledgmentMutation
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		mutation, err = q.negativeAcknowledgmentTx(
			tx,
			leaseID,
			workerID,
			workerSessionID,
			requireOwner,
		)

		return err
	}); err != nil {
		return fmt.Errorf("nak crawl lease: %w", err)
	}
	if mutation.staged {
		recoveryContext, cancel := automaticDiscoverySettlementRecoveryContext(ctx)
		defer cancel()
		if err := q.resolveAutomaticDiscoverySettlement(
			recoveryContext,
			leaseID,
		); err != nil {
			return fmt.Errorf("nak crawl lease: %w", err)
		}

		return fmt.Errorf("nak crawl lease: %w", errLeaseDispositionConflict)
	}
	if mutation.removed {
		q.workerLeases.remove(mutation.lease)
	}

	return nil
}

func leaseSweepInterval(leaseTTL time.Duration) time.Duration {
	return max(min(leaseTTL/4, negativeAcknowledgmentRetryDelay/2), time.Second)
}
