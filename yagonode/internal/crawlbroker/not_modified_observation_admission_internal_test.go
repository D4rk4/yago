package crawlbroker

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func rewriteNotModifiedLease(
	t *testing.T,
	queue *DurableOrderQueue,
	leaseID string,
	change func(*leaseRecord),
) {
	t.Helper()
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, found, err := queue.leases.Get(tx, vault.Key(leaseID))
		if err != nil {
			return fmt.Errorf("read not-modified lease: %w", err)
		}
		if !found {
			return errors.New("not-modified lease missing")
		}
		change(&record)

		return queue.leases.Put(tx, vault.Key(leaseID), record)
	}); err != nil {
		t.Fatalf("rewrite not-modified lease: %v", err)
	}
}

func authorizedNotModifiedSubmission(
	t *testing.T,
	name string,
) (*exchangeServer, chan crawlresults.IngestDelivery, *crawlrpc.IngestBatchMessage) {
	t.Helper()
	out := make(chan crawlresults.IngestDelivery, 1)
	server := newExchangeServer(memQueue(t), out)
	msg := ingestMessageForBatch(t, notModifiedIngestBatch())
	authorizeIngestMessage(t, server, msg, name)

	return server, out, msg
}

func TestSubmitIngestRefusesNotModifiedFromSettledLease(t *testing.T) {
	server, out, msg := authorizedNotModifiedSubmission(t, "not-modified-settled-lease")
	if _, err := server.AckOrder(t.Context(), &crawlrpc.OrderAck{
		LeaseId: msg.GetLeaseId(), WorkerId: msg.GetWorkerId(),
		WorkerSessionId: msg.GetWorkerSessionId(),
	}); err != nil {
		t.Fatalf("settle lease before not-modified submission: %v", err)
	}

	_, err := server.SubmitIngest(boundedSubmissionContext(t), msg)

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("settled-lease not-modified status = %v, want FailedPrecondition", err)
	}
	if len(out) != 0 {
		t.Fatal("not-modified observation reached the document ingest pipeline")
	}
}

func TestSubmitIngestRefusesNotModifiedFromReplacedSession(t *testing.T) {
	server, out, msg := authorizedNotModifiedSubmission(t, "not-modified-replaced-session")
	deactivateTestWorkerSession(t, server, testWorkerSessionID)
	activateTestWorkerSession(t, server, "worker", "replacement-worker-session")
	rewriteNotModifiedLease(t, server.queue, msg.GetLeaseId(), func(record *leaseRecord) {
		record.WorkerSessionID = testWorkerSessionID
	})

	_, err := server.SubmitIngest(boundedSubmissionContext(t), msg)

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("replaced-session not-modified status = %v, want FailedPrecondition", err)
	}
	if len(out) != 0 {
		t.Fatal("not-modified observation reached the document ingest pipeline")
	}
}

func TestSubmitIngestDefersNotModifiedWhenLeaseIsUnreadable(t *testing.T) {
	server, out, msg := authorizedNotModifiedSubmission(t, "not-modified-unreadable-lease")
	rewriteNotModifiedLease(t, server.queue, msg.GetLeaseId(), func(record *leaseRecord) {
		record.OrderData = []byte("{")
	})

	_, err := server.SubmitIngest(boundedSubmissionContext(t), msg)

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("unreadable-lease not-modified status = %v, want Unavailable", err)
	}
	if len(out) != 0 {
		t.Fatal("not-modified observation reached the document ingest pipeline")
	}
}
