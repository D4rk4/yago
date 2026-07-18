package crawlbroker

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

func TestSlowIngestReleasesLeaseMutationBeforeStorage(t *testing.T) {
	for _, deliveryTotal := range []int{1, 16} {
		t.Run(fmt.Sprintf("deliveries-%d", deliveryTotal), func(t *testing.T) {
			assertSlowIngestLeaseMutationAvailability(t, deliveryTotal)
		})
	}
}

type slowSnapshotIngest struct {
	queue      *DurableOrderQueue
	server     *exchangeServer
	leases     []string
	deliveries []crawlresults.IngestDelivery
	results    <-chan error
}

type blockedSnapshotStorage struct {
	entered     <-chan int
	release     chan struct{}
	releaseOnce sync.Once
	done        <-chan struct{}
}

func assertSlowIngestLeaseMutationAvailability(t *testing.T, deliveryTotal int) {
	t.Helper()
	ingest := startSlowSnapshotIngest(t, deliveryTotal)
	authorizeSnapshotDeliveries(t, ingest.deliveries)
	storage := startBlockedSnapshotStorage(t, ingest.deliveries)
	waitForSnapshotStorage(t, storage.entered, deliveryTotal)
	assertSnapshotLeaseWriterAvailable(t, ingest.queue)
	assertSnapshotHeartbeatAvailable(t, ingest.server, ingest.leases)
	storage.releaseAbsorption()
	waitForSnapshotIngestResults(t, ingest.results, deliveryTotal)
	storage.waitForStop(t)
}

func startSlowSnapshotIngest(t *testing.T, deliveryTotal int) slowSnapshotIngest {
	t.Helper()
	queue := memQueue(t)
	submissions := make(chan crawlresults.IngestDelivery, deliveryTotal)
	server := newExchangeServer(queue, submissions)
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	leases := make([]string, 0, deliveryTotal)
	results := make(chan error, deliveryTotal)
	for index := range deliveryTotal {
		leaseID := leaseOneForSession(
			t,
			queue,
			fmt.Sprintf("snapshot-%d", index),
			"worker",
			testWorkerSessionID,
		)
		leases = append(leases, leaseID)
		message := leaseSnapshotIngestMessage(t, leaseID, index)
		go func() {
			_, err := server.SubmitIngest(t.Context(), message)
			results <- err
		}()
	}
	deliveries := make([]crawlresults.IngestDelivery, 0, deliveryTotal)
	for range deliveryTotal {
		delivery := receiveSnapshotIngestDelivery(t, submissions)
		deliveries = append(deliveries, delivery)
	}

	return slowSnapshotIngest{
		queue:      queue,
		server:     server,
		leases:     leases,
		deliveries: deliveries,
		results:    results,
	}
}

func authorizeSnapshotDeliveries(
	t *testing.T,
	deliveries []crawlresults.IngestDelivery,
) {
	t.Helper()
	for _, delivery := range deliveries {
		if err := delivery.AuthorizeLeaseSnapshot(t.Context()); err != nil {
			t.Fatalf("authorize ingest snapshot: %v", err)
		}
	}
}

func receiveSnapshotIngestDelivery(
	t *testing.T,
	submissions <-chan crawlresults.IngestDelivery,
) crawlresults.IngestDelivery {
	t.Helper()
	select {
	case delivery := <-submissions:
		if delivery.AuthorizeLeaseSnapshot == nil {
			t.Fatal("snapshot authorization callback is nil")
		}
		if delivery.BeginMutation != nil || delivery.BeginMutationGroup != nil {
			t.Fatal("production ingest exposes a retained mutation callback")
		}

		return delivery
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ingest delivery")

		return crawlresults.IngestDelivery{}
	}
}

func startBlockedSnapshotStorage(
	t *testing.T,
	deliveries []crawlresults.IngestDelivery,
) *blockedSnapshotStorage {
	t.Helper()
	release := make(chan struct{})
	entered := make(chan int, 1)
	done := make(chan struct{})
	go func() {
		entered <- len(deliveries)
		<-release
		for _, delivery := range deliveries {
			_ = delivery.Ack(t.Context())
		}
		close(done)
	}()
	storage := &blockedSnapshotStorage{
		entered: entered,
		release: release,
		done:    done,
	}
	t.Cleanup(func() {
		storage.releaseAbsorption()
		select {
		case <-storage.done:
		case <-time.After(time.Second):
		}
	})

	return storage
}

func waitForSnapshotStorage(
	t *testing.T,
	entered <-chan int,
	deliveryTotal int,
) {
	t.Helper()
	select {
	case stored := <-entered:
		if stored != deliveryTotal {
			t.Fatalf("stored documents = %d, want %d", stored, deliveryTotal)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for document storage")
	}
}

func assertSnapshotLeaseWriterAvailable(t *testing.T, queue *DurableOrderQueue) {
	t.Helper()
	acquired := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		queue.leaseMutation.Lock()
		close(acquired)
		<-release
		queue.leaseMutation.Unlock()
		close(done)
	}()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("lease mutation writer blocked behind document storage")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("lease mutation writer did not stop")
	}
}

func assertSnapshotHeartbeatAvailable(
	t *testing.T,
	server *exchangeServer,
	leaseIDs []string,
) {
	t.Helper()
	heartbeatDone := make(chan error, 1)
	go func() {
		_, err := server.Heartbeat(t.Context(), &crawlrpc.WorkerHeartbeat{
			WorkerId:        "worker",
			WorkerSessionId: testWorkerSessionID,
			ActiveLeaseIds:  leaseIDs,
		})
		heartbeatDone <- err
	}()
	select {
	case err := <-heartbeatDone:
		if err != nil {
			t.Fatalf("heartbeat during document storage: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat blocked behind document storage")
	}
}

func (s *blockedSnapshotStorage) releaseAbsorption() {
	s.releaseOnce.Do(func() { close(s.release) })
}

func (s *blockedSnapshotStorage) waitForStop(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(time.Second):
		t.Fatal("snapshot absorption did not stop")
	}
}

func waitForSnapshotIngestResults(
	t *testing.T,
	results <-chan error,
	deliveryTotal int,
) {
	t.Helper()
	for range deliveryTotal {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("submit ingest: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for ingest acknowledgment")
		}
	}
}

func TestStaleLeaseSnapshotIsRejectedBeforeAbsorption(t *testing.T) {
	queue := memQueue(t)
	submissions := make(chan crawlresults.IngestDelivery, 1)
	server := newExchangeServer(queue, submissions)
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
	leaseID := leaseOneForSession(
		t,
		queue,
		"stale-snapshot",
		"worker",
		testWorkerSessionID,
	)
	message := leaseSnapshotIngestMessage(t, leaseID, 0)
	result := make(chan error, 1)
	go func() {
		_, err := server.SubmitIngest(t.Context(), message)
		result <- err
	}()
	delivery := <-submissions
	if _, err := server.AckOrder(t.Context(), &crawlrpc.OrderAck{
		LeaseId:         leaseID,
		WorkerId:        "worker",
		WorkerSessionId: testWorkerSessionID,
	}); err != nil {
		t.Fatalf("settle ingest lease: %v", err)
	}

	if err := delivery.AuthorizeLeaseSnapshot(t.Context()); !errors.Is(err, errLeaseLost) {
		t.Fatalf("stale snapshot authorization = %v, want lease loss", err)
	}
	if err := delivery.LeaseLost(t.Context()); err != nil {
		t.Fatalf("report stale snapshot: %v", err)
	}
	if err := <-result; status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale ingest status = %v, want FailedPrecondition", status.Code(err))
	}
}

func leaseSnapshotIngestMessage(
	t *testing.T,
	leaseID string,
	index int,
) *crawlrpc.IngestBatchMessage {
	t.Helper()
	url := fmt.Sprintf("https://example.test/snapshot/%d", index)
	data, err := yagocrawlcontract.MarshalIngestBatch(yagocrawlcontract.IngestBatch{
		SourceURL:  url,
		Provenance: []byte("admin"),
		Document: yagocrawlcontract.DocumentIngest{
			NormalizedURL: url,
			ExtractedText: "lease snapshot ingest body",
		},
	})
	if err != nil {
		t.Fatalf("marshal snapshot ingest: %v", err)
	}

	return &crawlrpc.IngestBatchMessage{
		LeaseId:         leaseID,
		WorkerId:        "worker",
		WorkerSessionId: testWorkerSessionID,
		BatchJson:       data,
	}
}
