package crawlbroker

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) leaseSettlementExpiryDue(
	ctx context.Context,
	now time.Time,
) (bool, error) {
	due := false
	err := q.vault.View(ctx, func(tx *vault.Txn) error {
		return q.leaseSettlementExpiry.Scan(tx, nil, func(
			key vault.Key,
			_ []byte,
		) (bool, error) {
			settled, err := leaseSettlementExpiryIdentity(key)
			if err != nil {
				return false, err
			}
			due = !time.Unix(0, settled).Add(leaseSettlementRetention).After(now)

			return false, nil
		})
	})
	if err != nil {
		return false, fmt.Errorf("inspect crawl lease settlement expiry: %w", err)
	}

	return due, nil
}

func (q *DurableOrderQueue) expiredLeaseSettlementCandidates(
	tx *vault.Txn,
	now time.Time,
) ([]expiredLeaseSettlementIndex, error) {
	expired := make([]expiredLeaseSettlementIndex, 0, maximumLeaseSettlementRetentionChunk)
	err := q.leaseSettlementExpiry.Scan(tx, nil, func(
		key vault.Key,
		leaseID []byte,
	) (bool, error) {
		settled, err := leaseSettlementExpiryIdentity(key)
		if err != nil {
			return false, err
		}
		if time.Unix(0, settled).Add(leaseSettlementRetention).After(now) {
			return false, nil
		}
		expired = append(expired, expiredLeaseSettlementIndex{
			key:     append(vault.Key(nil), key...),
			leaseID: string(leaseID),
			settled: settled,
		})

		return len(expired) < maximumLeaseSettlementRetentionChunk, nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan crawl lease settlement expiry: %w", err)
	}

	return expired, nil
}

func (q *DurableOrderQueue) deleteExpiredLeaseSettlementTx(
	tx *vault.Txn,
	candidate expiredLeaseSettlementIndex,
) (bool, error) {
	record, found, err := q.leaseSettlements.Get(tx, vault.Key(candidate.leaseID))
	if err != nil {
		return false, fmt.Errorf("read expired crawl lease settlement: %w", err)
	}
	retainedAt := record.SettledAtUnixNano
	if record.Terminal {
		retainedAt = record.FinalizedAtUnixNano
	}
	if !found || retainedAt != candidate.settled ||
		!bytes.Equal(leaseSettlementExpiryKey(record), candidate.key) {
		if _, err := q.leaseSettlementExpiry.Delete(tx, candidate.key); err != nil {
			return false, fmt.Errorf("delete stale crawl lease settlement expiry: %w", err)
		}

		return false, nil
	}
	if record.Terminal {
		return q.expireTerminalLeaseSettlementTx(tx, candidate, record)
	}
	_, leased, err := q.leases.Get(tx, vault.Key(candidate.leaseID))
	if err != nil {
		return false, fmt.Errorf("read expired crawl lease: %w", err)
	}
	if leased {
		return false, nil
	}
	if err := q.verifyExpiredLeaseSettlementIndex(
		tx,
		candidate.leaseID,
		record.Sequence,
	); err != nil {
		return false, err
	}
	return false, q.deleteLeaseSettlementRetentionTx(tx, candidate, record.Sequence)
}

func (q *DurableOrderQueue) verifyExpiredLeaseSettlementIndex(
	tx *vault.Txn,
	leaseID string,
	sequence uint64,
) error {
	indexedLeaseID, indexed, err := q.leaseSettlementOrder.Get(tx, orderKey(sequence))
	if err != nil {
		return fmt.Errorf("read expired crawl lease settlement index: %w", err)
	}
	if !indexed || !bytes.Equal(indexedLeaseID, []byte(leaseID)) {
		return fmt.Errorf("crawl lease settlement index mismatch")
	}

	return nil
}

func (q *DurableOrderQueue) deleteLeaseSettlementRetentionTx(
	tx *vault.Txn,
	candidate expiredLeaseSettlementIndex,
	sequence uint64,
) error {
	if _, err := q.leaseSettlements.Delete(tx, vault.Key(candidate.leaseID)); err != nil {
		return fmt.Errorf("delete expired crawl lease settlement: %w", err)
	}
	if _, err := q.leaseSettlementOrder.Delete(tx, orderKey(sequence)); err != nil {
		return fmt.Errorf("delete expired crawl lease settlement index: %w", err)
	}
	if _, err := q.completedControlTargets.Delete(tx, vault.Key(candidate.leaseID)); err != nil {
		return fmt.Errorf("delete expired crawl lease control target: %w", err)
	}
	if _, err := q.leaseSettlementExpiry.Delete(tx, candidate.key); err != nil {
		return fmt.Errorf("delete expired crawl lease settlement expiry: %w", err)
	}

	return nil
}
