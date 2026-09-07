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
	DiscoveryKey      string                               `json:"discovery_key,omitempty"`
	DiscoverySequence uint64                               `json:"discovery_sequence,omitempty"`
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

func randomLeaseID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)

	return hex.EncodeToString(buf)
}

func (q *DurableOrderQueue) leasePopForSession(
	ctx context.Context,
	workerID string,
	workerSessionID string,
) ([]byte, string, bool, error) {
	q.leaseMutation.Lock()
	defer q.leaseMutation.Unlock()
	if !q.workerLeaseCapacityAvailable(workerID, workerSessionID) {
		return nil, "", false, nil
	}
	leaseID := randomLeaseID()
	selected, found, err := q.claimPendingOrder(ctx, leaseID, workerID, workerSessionID)
	if err != nil {
		return nil, "", false, fmt.Errorf("lease crawl order: %w", err)
	}

	return selected.data, leaseID, found, nil
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
	staged, err := q.stageAutomaticDiscoveryAcknowledgmentForCompletion(
		ctx,
		leaseID,
		workerID,
		workerSessionID,
		requireOwner,
	)
	if err != nil {
		return leaseControlTarget{}, fmt.Errorf("ack crawl lease: %w", err)
	}
	settlementContext := ctx
	cancelSettlement := func() {}
	if staged {
		afterAutomaticDiscoverySettlementStage()
		settlementContext, cancelSettlement = automaticDiscoverySettlementRecoveryContext(ctx)
	}
	defer cancelSettlement()
	var target leaseControlTarget
	var removed leaseRecord
	removedFound := false
	settlementErr := q.vault.Update(settlementContext, func(tx *vault.Txn) error {
		var err error
		target, removed, removedFound, err = q.acknowledgeLeaseTx(
			tx,
			leaseID,
			workerID,
			workerSessionID,
			requireOwner,
		)

		return err
	})
	mainSettlementCommitted := settlementErr == nil
	if mainSettlementCommitted && removedFound {
		q.workerLeases.remove(removed)
	}
	resolution, recoveryErr := q.completeAutomaticDiscoverySettlement(
		settlementContext,
		leaseID,
	)
	if resolution.Acknowledged && (!mainSettlementCommitted || !removedFound) {
		q.workerLeases.remove(resolution.Intent.Lease)
	}
	if recoveryErr != nil {
		q.signal()

		return leaseControlTarget{}, recoveryErr
	}
	if settlementErr != nil && !resolution.Acknowledged {
		return leaseControlTarget{}, fmt.Errorf("ack crawl lease: %w", settlementErr)
	}
	if resolution.Acknowledged {
		target = resolution.Intent.Target
	}

	return target, nil
}

func (q *DurableOrderQueue) sweepExpired(ctx context.Context) error {
	now := nowFunc()
	if err := q.requeueLeasesMatching(ctx, func(record leaseRecord) bool {
		return record.ExpiresAtUnixNano <= now.UnixNano() &&
			!leaseRetainsCheckpointAffinity(record)
	}); err != nil {
		return err
	}
	if err := q.reclaimAbandonedLeases(ctx); err != nil {
		return err
	}
	if err := q.expireLeaseSettlements(ctx, now); err != nil {
		return err
	}

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
