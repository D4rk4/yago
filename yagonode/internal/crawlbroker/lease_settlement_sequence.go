package crawlbroker

import (
	"bytes"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (q *DurableOrderQueue) reserveLeaseSettlementSequenceTx(
	tx *vault.Txn,
) (uint64, error) {
	next, _, err := q.seq.Get(tx, leaseSettlementNextKey)
	if err != nil {
		return 0, fmt.Errorf("read crawl lease settlement sequence: %w", err)
	}
	migrationNext, _, err := q.seq.Get(tx, leaseSettlementMigrationNextKey)
	if err != nil {
		return 0, fmt.Errorf("read crawl lease settlement migration: %w", err)
	}
	first := next
	for {
		_, occupied, err := q.leaseSettlementOrder.Get(tx, orderKey(next))
		if err != nil {
			return 0, fmt.Errorf("read crawl lease settlement index: %w", err)
		}
		if !occupied {
			break
		}
		next++
	}
	if err := q.seq.Put(tx, leaseSettlementNextKey, next+1); err != nil {
		return 0, fmt.Errorf("advance crawl lease settlement sequence: %w", err)
	}
	if next == first && migrationNext == first {
		if err := q.seq.Put(tx, leaseSettlementMigrationNextKey, next+1); err != nil {
			return 0, fmt.Errorf(
				"advance crawl lease settlement migration: %w",
				err,
			)
		}
	}

	return next, nil
}

func (q *DurableOrderQueue) relocateAutomaticDiscoverySettlementSequenceTx(
	tx *vault.Txn,
	leaseID string,
	record leaseSettlementRecord,
) (leaseSettlementRecord, error) {
	indexedLeaseID, indexed, err := q.leaseSettlementOrder.Get(
		tx,
		orderKey(record.Sequence),
	)
	if err != nil {
		return leaseSettlementRecord{}, fmt.Errorf(
			"read crawl lease settlement index: %w",
			err,
		)
	}
	if !indexed || bytes.Equal(indexedLeaseID, []byte(leaseID)) {
		return record, nil
	}
	replacement, err := q.reserveLeaseSettlementSequenceTx(tx)
	if err != nil {
		return leaseSettlementRecord{}, err
	}
	previous := record
	record.Sequence = replacement
	if !record.Terminal {
		expiryKey := leaseSettlementExpiryKey(previous)
		indexedLeaseID, found, err := q.leaseSettlementExpiry.Get(tx, expiryKey)
		if err != nil {
			return leaseSettlementRecord{}, fmt.Errorf(
				"read crawl lease settlement expiry: %w",
				err,
			)
		}
		if found && bytes.Equal(indexedLeaseID, []byte(leaseID)) {
			if _, err := q.leaseSettlementExpiry.Delete(tx, expiryKey); err != nil {
				return leaseSettlementRecord{}, fmt.Errorf(
					"release crawl lease settlement expiry: %w",
					err,
				)
			}
		}
	}
	if err := q.leaseSettlements.Put(tx, vault.Key(leaseID), record); err != nil {
		return leaseSettlementRecord{}, fmt.Errorf(
			"relocate crawl lease settlement: %w",
			err,
		)
	}

	return record, nil
}
