package crawlbroker

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestWorkerLeaseCatalogRebuildsDurableAssignments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crawlbroker.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("open first storage: %v", err)
	}
	queue, err := newDurableOrderQueue(storage, time.Minute)
	if err != nil {
		t.Fatalf("open first queue: %v", err)
	}
	leaseOneForSession(t, queue, "active", "worker", "active-session")
	deferred := leaseOneForSession(t, queue, "deferred", "worker", "deferred-session")
	if err := queue.deferLeaseForOwner(
		t.Context(),
		deferred,
		"worker",
		"deferred-session",
	); err != nil {
		t.Fatalf("defer durable lease: %v", err)
	}
	leaseOne(t, queue, "legacy", "legacy-worker")
	assertWorkerLeaseCatalogCoherent(t, queue)
	if err := storage.Close(); err != nil {
		t.Fatalf("close first storage: %v", err)
	}

	storage, err = boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen storage: %v", err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	queue, err = newDurableOrderQueue(storage, time.Minute)
	if err != nil {
		t.Fatalf("reopen queue: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
	if !queue.workerLeases.reached("worker", "active-session", 1) ||
		queue.workerLeases.reached("worker", "deferred-session", 1) ||
		queue.workerLeases.reached("legacy-worker", "", 1) {
		t.Fatalf("reopened worker lease catalog = %+v", queue.workerLeases.active)
	}
}

func TestWorkerLeaseCatalogTracksDurableTransitions(t *testing.T) {
	t.Run("claim", assertWorkerLeaseCatalogTracksClaim)
	t.Run("acknowledgment", assertWorkerLeaseCatalogTracksAcknowledgment)
	t.Run("negative acknowledgment", assertWorkerLeaseCatalogTracksNegativeAcknowledgment)
	t.Run("terminal acknowledgment", assertWorkerLeaseCatalogTracksTerminalAcknowledgment)
	t.Run("terminal requeue", assertWorkerLeaseCatalogTracksTerminalRequeue)
	t.Run("session adoption", assertWorkerLeaseCatalogTracksSessionAdoption)
}

func assertWorkerLeaseCatalogTracksClaim(t *testing.T) {
	queue := memQueue(t)
	leaseOneForSession(t, queue, "claim", "worker", "session")
	assertWorkerLeaseCatalogCoherent(t, queue)
}

func assertWorkerLeaseCatalogTracksAcknowledgment(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "ack", "worker", "session")
	if _, err := queue.ackLeaseWithOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err != nil {
		t.Fatalf("acknowledge lease: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
}

func assertWorkerLeaseCatalogTracksNegativeAcknowledgment(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "nak", "worker", "session")
	if err := queue.deferLeaseForOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err != nil {
		t.Fatalf("defer lease: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
}

func assertWorkerLeaseCatalogTracksTerminalAcknowledgment(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "terminal-ack", "worker", "session")
	request := catalogTerminalLeaseRequest(t, queue, leaseID, leaseSettlementAcknowledged)
	if _, err := queue.prepareTerminalLeaseSettlement(
		t.Context(),
		leaseID,
		request,
	); err != nil {
		t.Fatalf("prepare terminal acknowledgment: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
}

func assertWorkerLeaseCatalogTracksTerminalRequeue(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "terminal-requeue", "worker", "session")
	request := catalogTerminalLeaseRequest(t, queue, leaseID, leaseSettlementRequeued)
	if _, err := queue.prepareTerminalLeaseSettlement(
		t.Context(),
		leaseID,
		request,
	); err != nil {
		t.Fatalf("prepare terminal requeue: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
}

func assertWorkerLeaseCatalogTracksSessionAdoption(t *testing.T) {
	queue := memQueue(t)
	leaseOneForSession(t, queue, "adopt", "worker", "old-session")
	if _, err := queue.adoptWorkerSession(
		t.Context(),
		"worker",
		"new-session",
	); err != nil {
		t.Fatalf("adopt worker session: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
	if queue.workerLeases.reached("worker", "old-session", 1) ||
		!queue.workerLeases.reached("worker", "new-session", 1) {
		t.Fatalf("adopted worker lease catalog = %+v", queue.workerLeases.active)
	}
}

func TestWorkerLeaseCatalogRetainsExpiredCheckpointAffinity(t *testing.T) {
	set := withClock(t)
	base := time.Unix(90_000, 0)
	set(base)
	queue := memQueue(t)
	queue.leaseTTL = time.Minute
	leaseID := leaseOneForSession(t, queue, "expired", "worker", "old-session")
	set(base.Add(2 * time.Minute))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("sweep expired checkpoint lease: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
	if !queue.workerLeases.reached("worker", "old-session", 1) {
		t.Fatal("expired assigned checkpoint lease stopped consuming capacity")
	}
	if _, err := queue.adoptWorkerSession(
		t.Context(),
		"worker",
		"new-session",
	); err != nil {
		t.Fatalf("adopt expired checkpoint lease: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
	if _, err := queue.ackLeaseWithOwner(
		t.Context(),
		leaseID,
		"worker",
		"new-session",
	); err != nil {
		t.Fatalf("settle adopted checkpoint lease: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
}

func TestWorkerLeaseCatalogExcludesDeferredRetryRequeue(t *testing.T) {
	set := withClock(t)
	base := time.Unix(100_000, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "retry", "worker", "session")
	if err := queue.deferLeaseForOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err != nil {
		t.Fatalf("defer retry lease: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
	set(base.Add(negativeAcknowledgmentRetryDelay))
	if err := queue.sweepExpired(t.Context()); err != nil {
		t.Fatalf("requeue deferred retry lease: %v", err)
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
	if _, found := leaseRecordFor(t, queue, leaseID); found {
		t.Fatal("requeued deferred lease remained persisted")
	}
}

func TestWorkerLeaseCatalogPreservesCommittedStateAfterMutationFailures(t *testing.T) {
	t.Run("claim", assertWorkerLeaseCatalogPreservesFailedClaim)
	t.Run("acknowledgment", assertWorkerLeaseCatalogPreservesFailedAcknowledgment)
	t.Run(
		"negative acknowledgment",
		assertWorkerLeaseCatalogPreservesFailedNegativeAcknowledgment,
	)
	t.Run(
		"terminal acknowledgment",
		assertWorkerLeaseCatalogPreservesFailedTerminalAcknowledgment,
	)
	t.Run("terminal requeue", assertWorkerLeaseCatalogPreservesFailedTerminalRequeue)
	t.Run("session adoption", assertWorkerLeaseCatalogPreservesFailedSessionAdoption)
}

func assertWorkerLeaseCatalogPreservesFailedClaim(t *testing.T) {
	fixture := scriptedQueue(t)
	if err := fixture.queue.Publish(t.Context(), testOrder("claim-failure")); err != nil {
		t.Fatalf("publish failed claim: %v", err)
	}
	fixture.engine.putErrors[leaseBucket] = fmt.Errorf("lease write failed")
	if _, _, _, err := fixture.queue.leasePopForSession(
		t.Context(),
		"worker",
		"session",
	); err == nil {
		t.Fatal("failed claim succeeded")
	}
	assertWorkerLeaseCatalogCoherent(t, fixture.queue)
}

func assertWorkerLeaseCatalogPreservesFailedAcknowledgment(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOneForSession(t, fixture.queue, "ack-failure", "worker", "session")
	fixture.engine.deleteErrors[leaseBucket] = fmt.Errorf("lease delete failed")
	if _, err := fixture.queue.ackLeaseWithOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err == nil {
		t.Fatal("failed acknowledgment succeeded")
	}
	assertWorkerLeaseCatalogCoherent(t, fixture.queue)
}

func assertWorkerLeaseCatalogPreservesFailedNegativeAcknowledgment(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOneForSession(t, fixture.queue, "nak-failure", "worker", "session")
	fixture.engine.putErrors[leaseBucket] = fmt.Errorf("lease write failed")
	if err := fixture.queue.deferLeaseForOwner(
		t.Context(),
		leaseID,
		"worker",
		"session",
	); err == nil {
		t.Fatal("failed negative acknowledgment succeeded")
	}
	assertWorkerLeaseCatalogCoherent(t, fixture.queue)
}

func assertWorkerLeaseCatalogPreservesFailedTerminalAcknowledgment(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOneForSession(
		t,
		fixture.queue,
		"terminal-ack-failure",
		"worker",
		"session",
	)
	request := catalogTerminalLeaseRequest(t, fixture.queue, leaseID, leaseSettlementAcknowledged)
	fixture.engine.deleteErrors[leaseBucket] = fmt.Errorf("lease delete failed")
	if _, err := fixture.queue.prepareTerminalLeaseSettlement(
		t.Context(),
		leaseID,
		request,
	); err == nil {
		t.Fatal("failed terminal acknowledgment succeeded")
	}
	assertWorkerLeaseCatalogCoherent(t, fixture.queue)
}

func assertWorkerLeaseCatalogPreservesFailedTerminalRequeue(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOneForSession(
		t,
		fixture.queue,
		"terminal-requeue-failure",
		"worker",
		"session",
	)
	request := catalogTerminalLeaseRequest(t, fixture.queue, leaseID, leaseSettlementRequeued)
	fixture.engine.putErrors[leaseBucket] = fmt.Errorf("lease write failed")
	if _, err := fixture.queue.prepareTerminalLeaseSettlement(
		t.Context(),
		leaseID,
		request,
	); err == nil {
		t.Fatal("failed terminal requeue succeeded")
	}
	assertWorkerLeaseCatalogCoherent(t, fixture.queue)
}

func assertWorkerLeaseCatalogPreservesFailedSessionAdoption(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseOneForSession(t, fixture.queue, "adoption-failure", "worker", "old-session")
	fixture.engine.putErrors[leaseBucket] = fmt.Errorf("lease write failed")
	if _, err := fixture.queue.adoptWorkerSession(
		t.Context(),
		"worker",
		"new-session",
	); err == nil {
		t.Fatal("failed session adoption succeeded")
	}
	assertWorkerLeaseCatalogCoherent(t, fixture.queue)
}

func TestWorkerLeaseCatalogReportsUnmatchedRemoval(t *testing.T) {
	catalog := &workerLeaseCatalog{active: make(map[workerLeaseSession]int)}
	record := leaseRecord{WorkerID: "worker", WorkerSessionID: "session"}
	if catalog.remove(record) {
		t.Fatal("missing assigned lease removal was reported as matched")
	}
	catalog.add(record)
	if !catalog.remove(record) {
		t.Fatal("persisted assigned lease removal was reported as missing")
	}
	if catalog.remove(record) {
		t.Fatal("assigned lease underflow was reported as matched")
	}
}

func TestWorkerLeaseCatalogSerializesConcurrentWorkerMutations(t *testing.T) {
	queue := memQueue(t)
	const workers = 8
	for index := 0; index < workers*workerLeaseCatalogOrdersPerWorker; index++ {
		if err := queue.Publish(
			t.Context(),
			testOrder(fmt.Sprintf("concurrent-%d", index)),
		); err != nil {
			t.Fatalf("publish concurrent order %d: %v", index, err)
		}
	}
	failures := make(chan error, workers)
	var group sync.WaitGroup
	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		workerID := fmt.Sprintf("worker-%d", workerIndex)
		sessionID := fmt.Sprintf("session-%d", workerIndex)
		group.Add(1)
		go func() {
			defer group.Done()
			if err := settleCatalogWorkerLeases(queue, workerID, sessionID); err != nil {
				failures <- err
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertWorkerLeaseCatalogCoherent(t, queue)
}

const workerLeaseCatalogOrdersPerWorker = 8

func settleCatalogWorkerLeases(
	queue *DurableOrderQueue,
	workerID string,
	sessionID string,
) error {
	for range workerLeaseCatalogOrdersPerWorker {
		_, leaseID, found, err := queue.leasePopForSession(
			context.Background(),
			workerID,
			sessionID,
		)
		if err != nil {
			return fmt.Errorf("claim: %w", err)
		}
		if !found {
			return errors.New("claim not found")
		}
		if _, err := queue.ackLeaseWithOwner(
			context.Background(),
			leaseID,
			workerID,
			sessionID,
		); err != nil {
			return fmt.Errorf("acknowledge %s: %w", leaseID, err)
		}
	}

	return nil
}

func catalogTerminalLeaseRequest(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
	outcome leaseSettlementOutcome,
) terminalLeaseRequest {
	t.Helper()
	record, found := leaseRecordFor(t, queue, leaseID)
	if !found {
		t.Fatalf("terminal catalog lease %q is missing", leaseID)
	}
	identity := sha256.Sum256(record.OrderData)

	return terminalLeaseRequest{
		Outcome:         outcome,
		OrderIdentity:   identity[:],
		WorkerID:        record.WorkerID,
		WorkerSessionID: record.WorkerSessionID,
		State:           yagocrawlcontract.CrawlRunFinished,
	}
}

func assertWorkerLeaseCatalogCoherent(t *testing.T, queue *DurableOrderQueue) {
	t.Helper()
	queue.leaseMutation.RLock()
	defer queue.leaseMutation.RUnlock()
	expected := make(map[workerLeaseSession]int)
	if err := queue.vault.View(t.Context(), func(tx *vault.Txn) error {
		return queue.leases.Scan(tx, nil, func(
			_ vault.Key,
			record leaseRecord,
		) (bool, error) {
			if !record.Deferred && record.WorkerID != "" && record.WorkerSessionID != "" {
				expected[workerLeaseSession{
					workerID:  record.WorkerID,
					sessionID: record.WorkerSessionID,
				}]++
			}

			return true, nil
		})
	}); err != nil {
		t.Fatalf("scan persisted worker leases: %v", err)
	}
	if !reflect.DeepEqual(queue.workerLeases.active, expected) {
		t.Fatalf(
			"worker lease catalog = %+v, persisted assignments = %+v",
			queue.workerLeases.active,
			expected,
		)
	}
}
