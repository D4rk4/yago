package crawlbroker

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestWorkerOrderLeaseCapacityParksUntilSettlement(t *testing.T) {
	queue := memQueue(t)
	workerID := "worker"
	workerSessionID := "session"
	leaseIDs := fillWorkerLeaseCapacity(t, queue, workerID, workerSessionID)
	if err := queue.Publish(t.Context(), testOrder("waiting")); err != nil {
		t.Fatalf("publish waiting order: %v", err)
	}
	select {
	case <-queue.notify:
	default:
	}
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	_, generation, err := server.activateWorkerSession(
		context.Background(), workerID, workerSessionID, func() {},
	)
	if err != nil {
		t.Fatalf("activate capacity worker: %v", err)
	}
	previousHook := beforeQueueWait
	t.Cleanup(func() { beforeQueueWait = previousHook })
	var waits atomic.Int32
	parked := make(chan struct{}, 1)
	beforeQueueWait = func() {
		waits.Add(1)
		select {
		case parked <- struct{}{}:
		default:
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	leased := make(chan error, 1)
	go func() {
		_, _, leaseErr := server.leaseNextForSession(
			ctx, workerID, workerSessionID, generation,
		)
		leased <- leaseErr
	}()
	select {
	case <-parked:
	case <-time.After(time.Second):
		t.Fatal("capacity-bound stream did not park")
	}
	time.Sleep(25 * time.Millisecond)
	if waits.Load() != 1 {
		t.Fatalf("capacity-bound stream wait loops = %d, want 1", waits.Load())
	}
	if _, err := queue.ackLeaseWithOwner(
		t.Context(), leaseIDs[0], workerID, workerSessionID,
	); err != nil {
		t.Fatalf("settle capacity lease: %v", err)
	}
	select {
	case err := <-leased:
		if err != nil {
			t.Fatalf("lease after capacity settlement: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("capacity settlement did not resume order stream")
	}
}

func fillWorkerLeaseCapacity(
	t *testing.T,
	queue *DurableOrderQueue,
	workerID string,
	workerSessionID string,
) []string {
	t.Helper()
	data, err := yagocrawlcontract.MarshalCrawlOrder(testOrder("capacity"))
	if err != nil {
		t.Fatalf("marshal capacity order: %v", err)
	}
	leaseIDs := make([]string, yagocrawlcontract.MaximumHeartbeatActiveLeases)
	err = queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		for index := range leaseIDs {
			leaseID := fmt.Sprintf("capacity-%04d", index)
			leaseIDs[index] = leaseID
			if err := queue.leases.Put(tx, vault.Key(leaseID), leaseRecord{
				OrderData: data, WorkerID: workerID, WorkerSessionID: workerSessionID,
				ExpiresAtUnixNano: time.Now().Add(time.Hour).UnixNano(),
			}); err != nil {
				return fmt.Errorf("store worker capacity lease: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("fill worker lease capacity: %v", err)
	}

	return leaseIDs
}
