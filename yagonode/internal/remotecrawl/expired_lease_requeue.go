package remotecrawl

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type expiredLease struct {
	key      vault.Key
	sequence uint64
}

func (b *Broker) expiredLeases(ctx context.Context, now time.Time) ([]expiredLease, error) {
	expired := make([]expiredLease, 0, MaximumRemoteCrawlBatch)
	if err := b.storage.View(ctx, func(tx *vault.Txn) error {
		if err := b.leaseExpiries.Scan(tx, nil, func(
			key vault.Key,
			record leaseExpiryRecord,
		) (bool, error) {
			expiresAt, valid := leaseExpiry(key)
			if !valid {
				return false, fmt.Errorf("remote crawl lease expiry key is malformed")
			}
			if expiresAt > now.UnixNano() {
				return false, nil
			}
			expired = append(expired, expiredLease{key: key, sequence: record.Sequence})

			return len(expired) < MaximumRemoteCrawlBatch, nil
		}); err != nil {
			return fmt.Errorf("scan expired remote crawl leases: %w", err)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("read expired remote crawl leases: %w", err)
	}

	return expired, nil
}

func (b *Broker) requeueExpiredLease(tx *vault.Txn, expiry expiredLease) error {
	current, found, err := b.orders.Get(tx, sequenceKey(expiry.sequence))
	if err != nil {
		return fmt.Errorf("read expired remote crawl lease: %w", err)
	}
	if !found || current.State != queueStateLeased || !sameLeaseExpiry(current, expiry) {
		if _, err := b.leaseExpiries.Delete(tx, expiry.key); err != nil {
			return fmt.Errorf("delete stale remote crawl lease expiry: %w", err)
		}

		return nil
	}
	if err := b.releaseLease(tx, current); err != nil {
		return err
	}
	current.State = queueStatePending
	current.Peer = ""
	current.LeaseUntil = 0
	if err := b.orders.Put(tx, sequenceKey(current.Sequence), current); err != nil {
		return fmt.Errorf("requeue expired remote crawl lease: %w", err)
	}
	if err := b.pending.Put(
		tx,
		sequenceKey(current.Sequence),
		pendingRecord{Sequence: current.Sequence},
	); err != nil {
		return fmt.Errorf("index requeued remote crawl order: %w", err)
	}

	return nil
}

func sameLeaseExpiry(record queueRecord, expiry expiredLease) bool {
	return string(leaseExpiryKey(record.LeaseUntil, record.Sequence)) == string(expiry.key)
}
