package crawlbroker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

const (
	leaseBucket vault.Name = "crawlorderleases"

	// DefaultLeaseTTL is how long a streamed order stays leased before a missing
	// heartbeat lets the sweeper reclaim and redeliver it.
	DefaultLeaseTTL = 2 * time.Minute
)

var (
	nowFunc    = time.Now
	newLeaseID = randomLeaseID
)

// leaseRecord is the durable state of an order leased to a worker: the order
// bytes to redeliver, the owning worker, and the deadline past which the lease
// is reclaimable.
type leaseRecord struct {
	OrderData         []byte `json:"order"`
	WorkerID          string `json:"worker"`
	ExpiresAtUnixNano int64  `json:"expires"`
}

type leaseRecordCodec struct{}

func (leaseRecordCodec) Encode(v leaseRecord) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode lease record: %w", err)
	}

	return raw, nil
}

func (leaseRecordCodec) Decode(raw []byte) (leaseRecord, error) {
	var rec leaseRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return leaseRecord{}, fmt.Errorf("decode lease record: %w", err)
	}

	return rec, nil
}

func randomLeaseID() (string, error) {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)

	return hex.EncodeToString(buf), nil
}

// leaseNext blocks until a pending order is available, then leases it to
// workerID and returns the order bytes with its lease id.
func (q *DurableOrderQueue) leaseNext(
	ctx context.Context,
	workerID string,
) ([]byte, string, error) {
	for {
		data, leaseID, ok, err := q.leasePop(ctx, workerID)
		if err != nil {
			return nil, "", err
		}
		if ok {
			return data, leaseID, nil
		}
		beforeQueueWait()
		select {
		case <-q.notify:
		case <-ctx.Done():
			return nil, "", fmt.Errorf("await crawl order: %w", ctx.Err())
		}
	}
}

func (q *DurableOrderQueue) leasePop(
	ctx context.Context,
	workerID string,
) ([]byte, string, bool, error) {
	leaseID, err := newLeaseID()
	if err != nil {
		return nil, "", false, err
	}

	var data []byte
	var key vault.Key
	found := false
	err = q.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := q.orders.Scan(tx, nil, func(k vault.Key, v []byte) (bool, error) {
			key = k
			data = append([]byte(nil), v...)
			found = true

			return false, nil
		}); err != nil {
			return fmt.Errorf("scan crawl orders: %w", err)
		}
		if !found {
			return nil
		}
		if _, err := q.orders.Delete(tx, key); err != nil {
			return fmt.Errorf("delete crawl order: %w", err)
		}
		record := leaseRecord{
			OrderData:         data,
			WorkerID:          workerID,
			ExpiresAtUnixNano: nowFunc().Add(q.leaseTTL).UnixNano(),
		}
		if err := q.leases.Put(tx, vault.Key(leaseID), record); err != nil {
			return fmt.Errorf("store crawl lease: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, "", false, fmt.Errorf("lease crawl order: %w", err)
	}

	return data, leaseID, found, nil
}

// ackLease settles a lease by deleting the order. Acking an unknown lease is a
// no-op so a duplicate ack is harmless.
func (q *DurableOrderQueue) ackLease(ctx context.Context, leaseID string) error {
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := q.leases.Delete(tx, vault.Key(leaseID)); err != nil {
			return fmt.Errorf("delete crawl lease: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("ack crawl lease: %w", err)
	}

	return nil
}

// requeueLease returns a leased order to the pending queue. Requeuing an unknown
// lease is a no-op.
func (q *DurableOrderQueue) requeueLease(ctx context.Context, leaseID string) error {
	requeued := false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		record, ok, err := q.leases.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read crawl lease: %w", err)
		}
		if !ok {
			return nil
		}
		if _, err := q.leases.Delete(tx, vault.Key(leaseID)); err != nil {
			return fmt.Errorf("delete crawl lease: %w", err)
		}
		if _, err := q.enqueueTx(tx, record.OrderData); err != nil {
			return err
		}
		requeued = true

		return nil
	}); err != nil {
		return fmt.Errorf("requeue crawl lease: %w", err)
	}
	if requeued {
		q.signal()
	}

	return nil
}

// heartbeat extends the deadline on every lease held by workerID.
func (q *DurableOrderQueue) heartbeat(ctx context.Context, workerID string) error {
	deadline := nowFunc().Add(q.leaseTTL).UnixNano()
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var keys []vault.Key
		var records []leaseRecord
		if err := q.leases.Scan(tx, nil, func(k vault.Key, record leaseRecord) (bool, error) {
			if record.WorkerID == workerID {
				record.ExpiresAtUnixNano = deadline
				keys = append(keys, k)
				records = append(records, record)
			}

			return true, nil
		}); err != nil {
			return fmt.Errorf("scan crawl leases: %w", err)
		}
		for i, key := range keys {
			if err := q.leases.Put(tx, key, records[i]); err != nil {
				return fmt.Errorf("extend crawl lease: %w", err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("heartbeat crawl leases: %w", err)
	}

	return nil
}

// sweepExpired returns every lease past its deadline to the pending queue.
func (q *DurableOrderQueue) sweepExpired(ctx context.Context) error {
	now := nowFunc().UnixNano()

	return q.requeueLeasesMatching(ctx, func(record leaseRecord) bool {
		return record.ExpiresAtUnixNano <= now
	})
}

// requeueAllLeases returns every lease to the pending queue, used at startup to
// reclaim orders leased by workers that a node restart disconnected.
func (q *DurableOrderQueue) requeueAllLeases(ctx context.Context) error {
	return q.requeueLeasesMatching(ctx, func(leaseRecord) bool { return true })
}

func (q *DurableOrderQueue) requeueLeasesMatching(
	ctx context.Context,
	match func(leaseRecord) bool,
) error {
	requeued := false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var keys []vault.Key
		var payloads [][]byte
		if err := q.leases.Scan(tx, nil, func(k vault.Key, record leaseRecord) (bool, error) {
			if match(record) {
				keys = append(keys, k)
				payloads = append(payloads, record.OrderData)
			}

			return true, nil
		}); err != nil {
			return fmt.Errorf("scan crawl leases: %w", err)
		}
		for i, key := range keys {
			if _, err := q.leases.Delete(tx, key); err != nil {
				return fmt.Errorf("delete crawl lease: %w", err)
			}
			if _, err := q.enqueueTx(tx, payloads[i]); err != nil {
				return err
			}
			requeued = true
		}

		return nil
	}); err != nil {
		return fmt.Errorf("requeue crawl leases: %w", err)
	}
	if requeued {
		q.signal()
	}

	return nil
}
