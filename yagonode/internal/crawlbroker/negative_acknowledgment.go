package crawlbroker

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	negativeAcknowledgmentRetryDelay = 5 * time.Second
)

func (q *DurableOrderQueue) deferLease(ctx context.Context, leaseID string) error {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	err := q.deferLeaseLocked(ctx, leaseDeferral{
		leaseID:    leaseID,
		retryAfter: negativeAcknowledgmentRetryDelay,
	})
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
	return q.deferLeaseForOwnerAfter(
		ctx,
		leaseID,
		workerID,
		workerSessionID,
		negativeAcknowledgmentRetryDelay,
	)
}

func (q *DurableOrderQueue) deferLeaseForOwnerAfter(
	ctx context.Context,
	leaseID string,
	workerID string,
	workerSessionID string,
	retryAfter time.Duration,
) error {
	if retryAfter <= 0 || retryAfter > yagocrawlcontract.MaximumCrawlBackoffDelay {
		return fmt.Errorf("invalid crawl order retry delay")
	}
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	err := q.deferLeaseLocked(ctx, leaseDeferral{
		leaseID:         leaseID,
		workerID:        workerID,
		workerSessionID: workerSessionID,
		requireOwner:    true,
		retryAfter:      retryAfter,
	})
	if err == nil {
		q.signal()
	}

	return err
}

func (q *DurableOrderQueue) deferLeaseLocked(ctx context.Context, deferral leaseDeferral) error {
	var mutation negativeAcknowledgmentMutation
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		mutation, err = q.negativeAcknowledgmentTxAfter(tx, deferral)

		return err
	}); err != nil {
		return fmt.Errorf("nak crawl lease: %w", err)
	}
	if mutation.staged {
		recoveryContext, cancel := automaticDiscoverySettlementRecoveryContext(ctx)
		defer cancel()
		if err := q.resolveAutomaticDiscoverySettlement(
			recoveryContext,
			deferral.leaseID,
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
