package crawlbroker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	leaseBucket vault.Name = "crawlorderleases"

	// DefaultLeaseTTL is how long a streamed order stays leased before a missing
	// heartbeat lets the sweeper reclaim and redeliver it.
	DefaultLeaseTTL = 2 * time.Minute
)

var (
	nowFunc                = time.Now
	newLeaseID             = randomLeaseID
	afterLeaseRequeueChunk = func() {}
)

const maximumLeaseRequeueChunk = 256

// leaseRecord is the durable state of an order leased to a worker: the order
// bytes to redeliver, the owning worker, and the deadline past which the lease
// is reclaimable.
type leaseRecord struct {
	OrderData         []byte                               `json:"order"`
	Priority          yagocrawlcontract.CrawlOrderPriority `json:"priority,omitempty"`
	WorkerID          string                               `json:"worker"`
	WorkerSessionID   string                               `json:"session,omitempty"`
	Deferred          bool                                 `json:"deferred,omitempty"`
	ExpiresAtUnixNano int64                                `json:"expires"`
}

type leaseRecordCodec struct{}

func (leaseRecordCodec) Encode(v leaseRecord) ([]byte, error) {
	raw, _ := json.Marshal(v)

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

func (q *DurableOrderQueue) leaseNext(ctx context.Context) ([]byte, error) {
	for {
		data, _, ok, err := q.leasePop(ctx, "worker")
		if err != nil {
			return nil, err
		}
		if ok {
			return data, nil
		}
		beforeQueueWait()
		select {
		case <-q.notify:
		case <-ctx.Done():
			return nil, fmt.Errorf("await crawl order: %w", ctx.Err())
		}
	}
}

func (q *DurableOrderQueue) leasePop(
	ctx context.Context,
	workerID string,
) ([]byte, string, bool, error) {
	return q.leasePopForSession(ctx, workerID, "")
}

func (q *DurableOrderQueue) leasePopForSession(
	ctx context.Context,
	workerID string,
	workerSessionID string,
) ([]byte, string, bool, error) {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	leaseID, err := newLeaseID()
	if err != nil {
		return nil, "", false, fmt.Errorf("create crawl lease identity: %w", err)
	}
	selected, found, err := q.claimPendingOrder(ctx, leaseID, workerID, workerSessionID)
	if err != nil {
		return nil, "", false, fmt.Errorf("lease crawl order: %w", err)
	}

	return selected.data, leaseID, found, nil
}

func (q *DurableOrderQueue) ackLease(ctx context.Context, leaseID string) error {
	_, err := q.ackLeaseWithTarget(ctx, leaseID)

	return err
}

func (q *DurableOrderQueue) ackLeaseWithTarget(
	ctx context.Context,
	leaseID string,
) (leaseControlTarget, error) {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	target, err := q.ackLeaseWithTargetLocked(ctx, leaseID, "", "", false)
	if err == nil {
		q.signal()
	}

	return target, err
}

func (q *DurableOrderQueue) ackLeaseWithOwner(
	ctx context.Context,
	leaseID string,
	workerID string,
	workerSessionID string,
) (leaseControlTarget, error) {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	target, err := q.ackLeaseWithTargetLocked(
		ctx,
		leaseID,
		workerID,
		workerSessionID,
		true,
	)
	if err == nil {
		q.signal()
	}

	return target, err
}

func (q *DurableOrderQueue) ackLeaseWithTargetLocked(
	ctx context.Context,
	leaseID string,
	workerID string,
	workerSessionID string,
	requireOwner bool,
) (leaseControlTarget, error) {
	var target leaseControlTarget
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		var err error
		target, err = q.acknowledgeLeaseTx(
			tx,
			leaseID,
			workerID,
			workerSessionID,
			requireOwner,
		)

		return err
	}); err != nil {
		return leaseControlTarget{}, fmt.Errorf("ack crawl lease: %w", err)
	}

	return target, nil
}

func (q *DurableOrderQueue) heartbeat(ctx context.Context, workerID string) error {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	now := nowFunc()
	q.mu.Lock()
	last, seen := q.extendedAt[workerID]
	q.mu.Unlock()
	if seen && now.Sub(last) < q.leaseTTL/4 {
		return nil
	}
	deadline := now.Add(q.leaseTTL).UnixNano()
	extended := false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		extended = false
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
		extended = len(keys) > 0

		return nil
	}); err != nil {
		return fmt.Errorf("heartbeat crawl leases: %w", err)
	}
	q.mu.Lock()
	if extended {
		q.extendedAt[workerID] = now
	} else {
		delete(q.extendedAt, workerID)
	}
	q.mu.Unlock()

	return nil
}

func (q *DurableOrderQueue) sweepExpired(ctx context.Context) error {
	now := nowFunc()
	if err := q.requeueLeasesMatching(ctx, func(record leaseRecord) bool {
		return record.ExpiresAtUnixNano <= now.UnixNano() &&
			!leaseRetainsCheckpointAffinity(record)
	}); err != nil {
		return err
	}
	if err := q.expireLeaseSettlements(ctx, now); err != nil {
		return err
	}
	q.mu.Lock()
	for workerID, extendedAt := range q.extendedAt {
		if now.Sub(extendedAt) >= q.leaseTTL {
			delete(q.extendedAt, workerID)
		}
	}
	q.mu.Unlock()

	return nil
}

func (q *DurableOrderQueue) requeueLeasesMatching(
	ctx context.Context,
	match func(leaseRecord) bool,
) error {
	keys, err := q.matchingLeaseKeys(ctx, match)
	if err != nil {
		return err
	}
	for offset := 0; offset < len(keys); offset += maximumLeaseRequeueChunk {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("requeue crawl leases: %w", err)
		}
		limit := min(offset+maximumLeaseRequeueChunk, len(keys))
		requeued, err := q.requeueLeaseChunk(ctx, keys[offset:limit], match)
		if err != nil {
			return fmt.Errorf("requeue crawl leases: %w", err)
		}
		if requeued {
			q.signal()
		}
		afterLeaseRequeueChunk()
	}

	return nil
}
