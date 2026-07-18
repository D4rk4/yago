package crawlbroker

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) leaseSettlementMigrationBounds(
	ctx context.Context,
) (uint64, uint64, error) {
	var next uint64
	var cursor uint64
	err := q.vault.View(ctx, func(tx *vault.Txn) error {
		var err error
		next, _, err = q.seq.Get(tx, leaseSettlementNextKey)
		if err != nil {
			return fmt.Errorf("read crawl lease settlement sequence: %w", err)
		}
		cursor, _, err = q.seq.Get(tx, leaseSettlementMigrationNextKey)
		if err != nil {
			return fmt.Errorf("read crawl lease settlement migration: %w", err)
		}

		return nil
	})
	if err != nil {
		return 0, 0, fmt.Errorf("inspect crawl lease settlement retention: %w", err)
	}

	return next, cursor, nil
}

func (q *DurableOrderQueue) migrateLeaseSettlementChunkTx(
	tx *vault.Txn,
	cursor uint64,
	next uint64,
	observedAt time.Time,
) error {
	migrationCursor := cursor
	for inspected := 0; migrationCursor < next && inspected < maximumLeaseSettlementRetentionChunk; inspected++ {
		if err := q.migrateLeaseSettlementAtSequence(tx, migrationCursor, observedAt); err != nil {
			return err
		}
		migrationCursor++
	}
	if err := q.seq.Put(tx, leaseSettlementMigrationNextKey, migrationCursor); err != nil {
		return fmt.Errorf("advance crawl lease settlement migration: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) migrateLeaseSettlementAtSequence(
	tx *vault.Txn,
	sequence uint64,
	observedAt time.Time,
) error {
	leaseID, found, err := q.leaseSettlementOrder.Get(tx, orderKey(sequence))
	if err != nil {
		return fmt.Errorf("read crawl lease settlement index: %w", err)
	}
	if !found {
		return nil
	}
	record, settlementFound, err := q.leaseSettlements.Get(tx, vault.Key(leaseID))
	if err != nil {
		return fmt.Errorf("read crawl lease settlement: %w", err)
	}
	if !settlementFound {
		if _, err := q.leaseSettlementOrder.Delete(tx, orderKey(sequence)); err != nil {
			return fmt.Errorf("delete orphan crawl lease settlement index: %w", err)
		}

		return nil
	}
	if record.Sequence != sequence {
		return fmt.Errorf("crawl lease settlement sequence mismatch")
	}
	if record.SettledAtUnixNano != 0 {
		return nil
	}
	record.SettledAtUnixNano = observedAt.UnixNano()
	if err := q.leaseSettlements.Put(tx, vault.Key(leaseID), record); err != nil {
		return fmt.Errorf("migrate crawl lease settlement time: %w", err)
	}
	if record.Terminal {
		return nil
	}
	if err := q.leaseSettlementExpiry.Put(
		tx,
		leaseSettlementExpiryKey(record),
		leaseID,
	); err != nil {
		return fmt.Errorf("migrate crawl lease settlement expiry: %w", err)
	}

	return nil
}
