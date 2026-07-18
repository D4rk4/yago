package crawlbroker

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const maximumLeaseSettlementRetentionChunk = 256

type expiredLeaseSettlementIndex struct {
	key     vault.Key
	leaseID string
	settled int64
}

func leaseSettlementExpiryKey(record leaseSettlementRecord) vault.Key {
	retainedAt := record.SettledAtUnixNano
	if record.Terminal {
		retainedAt = record.FinalizedAtUnixNano
	}
	if retainedAt <= 0 {
		return nil
	}
	key := make(vault.Key, 16)
	big.NewInt(retainedAt).FillBytes(key[:8])
	binary.BigEndian.PutUint64(key[8:], record.Sequence)

	return key
}

func leaseSettlementExpiryIdentity(key vault.Key) (int64, error) {
	if len(key) != 16 {
		return 0, fmt.Errorf("invalid crawl lease settlement expiry identity")
	}
	encoded := new(big.Int).SetBytes(key[:8])
	if !encoded.IsInt64() || encoded.Sign() <= 0 {
		return 0, fmt.Errorf("invalid crawl lease settlement expiry time")
	}
	settled := encoded.Int64()

	return settled, nil
}

func (q *DurableOrderQueue) expireLeaseSettlements(
	ctx context.Context,
	now time.Time,
) error {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	if err := q.migrateLeaseSettlementExpiry(ctx, now); err != nil {
		return err
	}

	return q.deleteExpiredLeaseSettlements(ctx, now)
}

func (q *DurableOrderQueue) migrateLeaseSettlementExpiry(
	ctx context.Context,
	observedAt time.Time,
) error {
	next, cursor, err := q.leaseSettlementMigrationBounds(ctx)
	if err != nil {
		return err
	}
	if cursor >= next {
		return nil
	}
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		return q.migrateLeaseSettlementChunkTx(tx, cursor, next, observedAt)
	}); err != nil {
		return fmt.Errorf("migrate crawl lease settlement retention: %w", err)
	}

	return nil
}

func (q *DurableOrderQueue) deleteExpiredLeaseSettlements(
	ctx context.Context,
	now time.Time,
) error {
	due, err := q.leaseSettlementExpiryDue(ctx, now)
	if err != nil {
		return err
	}
	if !due {
		return nil
	}
	requeued := false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		expired, err := q.expiredLeaseSettlementCandidates(tx, now)
		if err != nil {
			return err
		}
		requeued = false
		for _, candidate := range expired {
			changed, err := q.deleteExpiredLeaseSettlementTx(tx, candidate)
			if err != nil {
				return err
			}
			requeued = requeued || changed
		}
		return nil
	}); err != nil {
		return fmt.Errorf("expire crawl lease settlement retention: %w", err)
	}
	if requeued {
		q.signal()
	}

	return nil
}
