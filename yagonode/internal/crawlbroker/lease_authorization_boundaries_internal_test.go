package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestProgressAndIngestRejectInvalidLeaseSurfaces(t *testing.T) {
	t.Run("progress storage failure", func(t *testing.T) {
		fixture := scriptedQueue(t)
		server := newExchangeServer(fixture.queue, make(chan crawlresults.IngestDelivery))
		leaseID := leaseOneForSession(
			t,
			fixture.queue,
			"progress-storage",
			"worker",
			testWorkerSessionID,
		)
		activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
		fixture.engine.buckets[leaseBucket][leaseID] = []byte("{")
		_, err := server.ReportProgress(t.Context(), &crawlrpc.CrawlProgressReport{
			LeaseId: leaseID, WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
			RunId: []byte("admin"), State: crawlrpc.CrawlRunState_CRAWL_RUN_STATE_RUNNING,
		})
		if status.Code(err) != codes.Internal {
			t.Fatalf("progress status = %v, want Internal", status.Code(err))
		}
	})

	t.Run("malformed ingest", func(t *testing.T) {
		server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
		_, err := server.SubmitIngest(t.Context(), &crawlrpc.IngestBatchMessage{
			WorkerId: "worker", WorkerSessionId: testWorkerSessionID,
			BatchJson: []byte("not json"),
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("malformed ingest status = %v", status.Code(err))
		}
	})

	t.Run("empty ingest lease", func(t *testing.T) {
		data, err := yagocrawlcontract.MarshalIngestBatch(yagocrawlcontract.IngestBatch{
			SourceURL: "https://example.test/no-lease",
		})
		if err != nil {
			t.Fatalf("marshal ingest: %v", err)
		}
		server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
		_, err = server.SubmitIngest(t.Context(), &crawlrpc.IngestBatchMessage{
			WorkerId: "worker", WorkerSessionId: testWorkerSessionID, BatchJson: data,
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("empty lease status = %v", status.Code(err))
		}
	})

	t.Run("session lost after ingest admission", func(t *testing.T) {
		ingest := make(chan crawlresults.IngestDelivery, 1)
		server := newExchangeServer(memQueue(t), ingest)
		message := ingestMessage(t, "https://example.test/session-loss")
		authorizeIngestMessage(t, server, message, "session-loss")
		done := make(chan error, 1)
		go func() {
			_, err := server.SubmitIngest(context.Background(), message)
			done <- err
		}()
		delivery := <-ingest
		deactivateTestWorkerSession(t, server, testWorkerSessionID)
		activateTestWorkerSession(t, server, "worker", "replacement-session")
		if err := delivery.AuthorizeLeaseSnapshot(t.Context()); !errors.Is(err, errLeaseLost) {
			t.Fatalf("validation error = %v", err)
		}
		if err := delivery.LeaseLost(t.Context()); err != nil {
			t.Fatalf("settle lost lease: %v", err)
		}
		if err := <-done; status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("session-loss status = %v", status.Code(err))
		}
	})
}

func TestLeaseAuthorizationValidatesOrderBindingAndStorage(t *testing.T) {
	t.Run("run optional", func(t *testing.T) {
		queue := memQueue(t)
		leaseID := leaseOneForSession(t, queue, "optional-run", "worker", testWorkerSessionID)
		if err := queue.verifyLeaseAuthorization(t.Context(), leaseAuthorization{
			LeaseID: leaseID, WorkerID: "worker", WorkerSessionID: testWorkerSessionID,
		}); err != nil {
			t.Fatalf("optional run authorization: %v", err)
		}
	})

	t.Run("corrupt order", func(t *testing.T) {
		queue := memQueue(t)
		leaseID := leaseOneForSession(t, queue, "corrupt-order", "worker", testWorkerSessionID)
		if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			record, _, err := queue.leases.Get(tx, vault.Key(leaseID))
			if err != nil {
				return fmt.Errorf("read corrupt order fixture: %w", err)
			}
			record.OrderData = []byte("{")

			if err := queue.leases.Put(tx, vault.Key(leaseID), record); err != nil {
				return fmt.Errorf("store corrupt order fixture: %w", err)
			}

			return nil
		}); err != nil {
			t.Fatalf("corrupt order fixture: %v", err)
		}
		if err := queue.verifyLeaseAuthorization(t.Context(), leaseAuthorization{
			LeaseID: leaseID, WorkerID: "worker", WorkerSessionID: testWorkerSessionID,
			RunID: testOrderRunID,
		}); err == nil {
			t.Fatal("corrupt order was authorized")
		}
	})

	t.Run("run mismatch", func(t *testing.T) {
		queue := memQueue(t)
		leaseID := leaseOneForSession(t, queue, "run-mismatch", "worker", testWorkerSessionID)
		if err := queue.verifyLeaseAuthorization(t.Context(), leaseAuthorization{
			LeaseID: leaseID, WorkerID: "worker", WorkerSessionID: testWorkerSessionID,
			RunID: "deadbeef",
		}); !errors.Is(err, errLeaseLost) {
			t.Fatalf("run mismatch error = %v", err)
		}
	})
}

func TestWorkerSessionAdoptionSurfacesDurableLeaseFailures(t *testing.T) {
	t.Run("scan", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.scanErrors[leaseBucket] = errors.New("scan failed")
		if _, err := fixture.queue.adoptWorkerSession(
			t.Context(),
			"worker",
			testWorkerSessionID,
		); err == nil {
			t.Fatal("lease scan failure was hidden")
		}
	})

	t.Run("write", func(t *testing.T) {
		fixture := scriptedQueue(t)
		leaseOne(t, fixture.queue, "adoption-write", "worker")
		fixture.engine.putErrors[leaseBucket] = errors.New("write failed")
		if _, err := fixture.queue.adoptWorkerSession(
			t.Context(),
			"worker",
			testWorkerSessionID,
		); err == nil {
			t.Fatal("lease adoption write failure was hidden")
		}
	})
}

func TestWorkerSessionAdoptionResetsResultsAcrossTransactionReplay(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOne(t, fixture.queue, "adoption-replay", "worker")
	fixture.engine.replayNext = true
	adopted, err := fixture.queue.adoptWorkerSession(
		t.Context(),
		"worker",
		testWorkerSessionID,
	)
	if err != nil {
		t.Fatalf("adopt replayed worker session: %v", err)
	}
	if len(adopted) != 1 || adopted[0].LeaseID != leaseID {
		t.Fatalf("replayed adoption = %+v, want one lease %q", adopted, leaseID)
	}
}

func TestLeaseDurationMillisecondsBoundsWireValue(t *testing.T) {
	for _, test := range []struct {
		duration time.Duration
		want     uint64
	}{
		{duration: 0, want: 0},
		{duration: -time.Second, want: 0},
		{duration: time.Nanosecond, want: 1},
		{duration: 2 * time.Millisecond, want: 2},
	} {
		if got := durationMilliseconds(test.duration); got != test.want {
			t.Fatalf("duration %v = %d ms, want %d", test.duration, got, test.want)
		}
	}
}
