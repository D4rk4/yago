package crawlbroker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type ingestDeliveryTestStream struct {
	deliveries <-chan crawlresults.IngestDelivery
}

func (s ingestDeliveryTestStream) Receive() <-chan crawlresults.IngestDelivery {
	return s.deliveries
}

func TestSubmitIngestReportsRealLeaseLossThroughConsumer(t *testing.T) {
	queue := memQueue(t)
	submissions := make(chan crawlresults.IngestDelivery, 1)
	server := newExchangeServer(queue, submissions)
	message := ingestMessage(t, "https://example.test/lost-ingest-lease")
	authorizeIngestMessage(t, server, message, "lost-ingest-lease")
	result := submitIngestForAuthorizationTest(server, message)
	delivery := receiveIngestForAuthorizationTest(t, submissions)
	if _, err := server.AckOrder(t.Context(), &crawlrpc.OrderAck{
		LeaseId: message.GetLeaseId(), WorkerId: message.GetWorkerId(),
		WorkerSessionId: message.GetWorkerSessionId(),
	}); err != nil {
		t.Fatalf("settle lease before ingest authorization: %v", err)
	}
	runIngestDeliveryForAuthorizationTest(t, delivery)
	if err := <-result; status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("lost lease status = %v, want FailedPrecondition", status.Code(err))
	}
}

func TestSubmitIngestNaksStorageAuthorizationFailureWithoutRevokingLease(t *testing.T) {
	queue := memQueue(t)
	submissions := make(chan crawlresults.IngestDelivery, 1)
	server := newExchangeServer(queue, submissions)
	message := ingestMessage(t, "https://example.test/storage-ingest-authorization")
	authorizeIngestMessage(t, server, message, "storage-ingest-authorization")
	before, found := leaseRecordFor(t, queue, message.GetLeaseId())
	if !found {
		t.Fatal("authorized lease missing before submission")
	}
	result := submitIngestForAuthorizationTest(server, message)
	delivery := receiveIngestForAuthorizationTest(t, submissions)
	if err := queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		record, found, err := queue.leases.Get(tx, vault.Key(message.GetLeaseId()))
		if err != nil {
			return fmt.Errorf("read ingest lease: %w", err)
		}
		if !found {
			return errors.New("authorized lease missing during test")
		}
		record.OrderData = []byte("{")

		return queue.leases.Put(tx, vault.Key(message.GetLeaseId()), record)
	}); err != nil {
		t.Fatalf("make lease-order authorization unreadable: %v", err)
	}
	runIngestDeliveryForAuthorizationTest(t, delivery)
	if err := <-result; status.Code(err) != codes.Unavailable {
		t.Fatalf("storage authorization status = %v, want Unavailable", status.Code(err))
	}
	after, found := leaseRecordFor(t, queue, message.GetLeaseId())
	if !found || after.WorkerID != before.WorkerID ||
		after.WorkerSessionID != before.WorkerSessionID || after.Deferred {
		t.Fatalf(
			"transient authorization changed live lease: before=%+v after=%+v found=%v",
			before,
			after,
			found,
		)
	}
}

func submitIngestForAuthorizationTest(
	server *exchangeServer,
	message *crawlrpc.IngestBatchMessage,
) <-chan error {
	result := make(chan error, 1)
	go func() {
		_, err := server.SubmitIngest(context.Background(), message)
		result <- err
	}()

	return result
}

func receiveIngestForAuthorizationTest(
	t *testing.T,
	deliveries <-chan crawlresults.IngestDelivery,
) crawlresults.IngestDelivery {
	t.Helper()
	select {
	case delivery := <-deliveries:
		return delivery
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ingest delivery")
		return crawlresults.IngestDelivery{}
	}
}

func runIngestDeliveryForAuthorizationTest(
	t *testing.T,
	delivery crawlresults.IngestDelivery,
) {
	t.Helper()
	deliveries := make(chan crawlresults.IngestDelivery, 1)
	deliveries <- delivery
	close(deliveries)
	consumer := crawlresults.NewIngestConsumer(
		ingestDeliveryTestStream{deliveries: deliveries}, nil, nil, nil,
	)
	consumer.Run(t.Context())
}
