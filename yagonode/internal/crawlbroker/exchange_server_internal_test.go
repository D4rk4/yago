package crawlbroker

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

type fakeOrderStream struct {
	grpc.ServerStream
	ctx     context.Context
	sent    []*crawlrpc.CrawlOrderMessage
	sendErr error
	onSend  func()
}

func (s *fakeOrderStream) Context() context.Context { return s.ctx }

func (s *fakeOrderStream) Send(msg *crawlrpc.CrawlOrderMessage) error {
	if s.onSend != nil {
		s.onSend()
	}
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sent = append(s.sent, msg)

	return nil
}

func TestStreamOrdersDeliversQueuedOrder(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if err := queue.Publish(context.Background(), testOrder("stream")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream := &fakeOrderStream{ctx: ctx, onSend: cancel}
	_ = server.StreamOrders(&crawlrpc.WorkerRegistration{
		WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
	}, stream)

	if len(stream.sent) != 1 {
		t.Fatalf("sent %d orders, want 1", len(stream.sent))
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(stream.sent[0].GetOrderJson())
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if order.Profile.Name != "stream" {
		t.Fatalf("order = %q, want stream", order.Profile.Name)
	}
}

func TestStreamOrdersKeepsLeaseOnSendError(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if err := queue.Publish(context.Background(), testOrder("requeue")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	stream := &fakeOrderStream{ctx: context.Background(), sendErr: errors.New("stream broken")}
	if err := server.StreamOrders(
		&crawlrpc.WorkerRegistration{
			WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
		},
		stream,
	); err == nil {
		t.Fatal("expected send error")
	}

	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want failed delivery held by its lease", n)
	}
	leasedOrders, err := queue.leasedOrdersForWorker(context.Background(), "w1")
	if err != nil {
		t.Fatalf("read worker leases: %v", err)
	}
	if len(leasedOrders) != 1 {
		t.Fatalf("leased orders = %d, want 1", len(leasedOrders))
	}
}

func TestStreamOrdersReturnsWhenContextCancelled(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.StreamOrders(
		&crawlrpc.WorkerRegistration{
			WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
		},
		&fakeOrderStream{ctx: ctx},
	); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestStreamOrdersReturnsLiveStorageFailureAsInternal(t *testing.T) {
	fixture := scriptedQueue(t)
	fixture.engine.buckets[seqBucket][string(priorityBurstKey)] = []byte{1}
	server := newExchangeServer(fixture.queue, make(chan crawlresults.IngestDelivery))
	err := server.StreamOrders(
		&crawlrpc.WorkerRegistration{
			WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
		},
		&fakeOrderStream{ctx: context.Background()},
	)
	if status.Code(err) != codes.Internal {
		t.Fatalf("live storage stream status = %v, want Internal", status.Code(err))
	}
}

func TestStreamOrdersKeepsInFlightLeaseOnDisconnect(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if err := queue.Publish(context.Background(), testOrder("reconnect")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream := &fakeOrderStream{ctx: ctx, onSend: cancel}
	_ = server.StreamOrders(&crawlrpc.WorkerRegistration{
		WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
	}, stream)
	if len(stream.sent) != 1 {
		t.Fatalf("sent %d orders, want the order delivered before the drop", len(stream.sent))
	}

	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want the in-flight order held across disconnect", n)
	}
	if _, ok := leaseRecordFor(t, queue, stream.sent[0].GetLeaseId()); !ok {
		t.Fatal("in-flight lease was not retained across disconnect")
	}
}

func TestReleaseWorkerLeavesLeaseAcrossOverlappingStreams(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	leaseID := leaseOne(t, queue, "held", "w1")

	server.control.register("w1")
	server.control.register("w1")
	queue.extendedAt["w1"] = time.Now()
	server.releaseWorker("w1")
	if _, ok := leaseRecordFor(t, queue, leaseID); !ok {
		t.Fatal("lease removed while a second stream is still connected")
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want the lease held while a stream lives", n)
	}
	if _, found := queue.extendedAt["w1"]; !found {
		t.Fatal("heartbeat state removed while a second stream is connected")
	}

	server.releaseWorker("w1")
	if _, ok := leaseRecordFor(t, queue, leaseID); !ok {
		t.Fatal("lease removed after the last stream disconnected")
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want the lease held after disconnect", n)
	}
	if _, found := queue.extendedAt["w1"]; !found {
		t.Fatal("heartbeat state was dropped while the worker may reconnect")
	}
}

func TestStreamOrdersRejectsEmptyWorkerID(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	err := server.StreamOrders(
		&crawlrpc.WorkerRegistration{},
		&fakeOrderStream{ctx: context.Background()},
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestSubmitIngestAbsorbsBatch(t *testing.T) {
	out := make(chan crawlresults.IngestDelivery)
	queue := memQueue(t)
	server := newExchangeServer(queue, out)
	msg := ingestMessage(t, "https://example.org/a")
	authorizeIngestMessage(t, server, msg, "ingest-a")
	go func() {
		delivery := <-out
		if delivery.BeginMutation != nil || delivery.BeginMutationGroup != nil {
			_ = delivery.LeaseLost(context.Background())

			return
		}
		if err := delivery.AuthorizeLeaseSnapshot(context.Background()); err == nil {
			_ = delivery.Ack(context.Background())
		} else {
			_ = delivery.LeaseLost(context.Background())
		}
	}()

	if _, err := server.SubmitIngest(context.Background(), msg); err != nil {
		t.Fatalf("submit ingest: %v", err)
	}
}

func TestSubmitIngestReportsSaturation(t *testing.T) {
	out := make(chan crawlresults.IngestDelivery)
	queue := memQueue(t)
	server := newExchangeServer(queue, out)
	msg := ingestMessage(t, "https://example.org/b")
	authorizeIngestMessage(t, server, msg, "ingest-b")
	go func() {
		delivery := <-out
		if err := delivery.AuthorizeLeaseSnapshot(context.Background()); err == nil {
			_ = delivery.Nak(context.Background())
		}
	}()

	_, err := server.SubmitIngest(context.Background(), msg)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("error = %v, want Unavailable", err)
	}
}

func TestSubmitIngestRejectsMalformedBatch(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	_, err := server.SubmitIngest(
		context.Background(),
		&crawlrpc.IngestBatchMessage{BatchJson: []byte("not json")},
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestSubmitIngestRejectsBatchAboveContractLimit(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	_, err := server.SubmitIngest(
		context.Background(),
		&crawlrpc.IngestBatchMessage{
			BatchJson:       make([]byte, yagocrawlcontract.MaximumIngestBatchBytes+1),
			WorkerId:        "worker",
			WorkerSessionId: testWorkerSessionID,
		},
	)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestSubmitIngestAcceptsBatchAtContractLimit(t *testing.T) {
	out := make(chan crawlresults.IngestDelivery)
	queue := memQueue(t)
	server := newExchangeServer(queue, out)
	message := ingestMessage(t, "https://example.org/maximum")
	authorizeIngestMessage(t, server, message, "maximum-ingest")
	message.BatchJson = append(
		message.BatchJson,
		bytes.Repeat(
			[]byte(" "),
			yagocrawlcontract.MaximumIngestBatchBytes-len(message.BatchJson),
		)...,
	)
	received := make(chan int, 1)
	go func() {
		delivery := <-out
		received <- delivery.BatchJSONSize
		_ = delivery.Ack(context.Background())
	}()
	if _, err := server.SubmitIngest(context.Background(), message); err != nil {
		t.Fatalf("submit maximum ingest: %v", err)
	}
	if size := <-received; size != yagocrawlcontract.MaximumIngestBatchBytes {
		t.Fatalf("batch JSON size = %d, want %d",
			size, yagocrawlcontract.MaximumIngestBatchBytes)
	}
}

func TestSubmitIngestReturnsWhenContextCancelledBeforeDelivery(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msg := ingestMessage(t, "https://example.org/c")
	authorizeIngestMessage(t, server, msg, "ingest-c")
	if _, err := server.SubmitIngest(ctx, msg); err == nil {
		t.Fatal("expected cancellation error before delivery")
	}
}

func TestSubmitIngestReturnsWhenContextCancelledAwaitingResult(t *testing.T) {
	out := make(chan crawlresults.IngestDelivery)
	queue := memQueue(t)
	server := newExchangeServer(queue, out)
	msg := ingestMessage(t, "https://example.org/d")
	authorizeIngestMessage(t, server, msg, "ingest-d")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-out
		cancel()
	}()
	if _, err := server.SubmitIngest(ctx, msg); err == nil {
		t.Fatal("expected cancellation error awaiting result")
	}
}

func ingestMessage(t *testing.T, sourceURL string) *crawlrpc.IngestBatchMessage {
	t.Helper()
	data, err := yagocrawlcontract.MarshalIngestBatch(
		yagocrawlcontract.IngestBatch{SourceURL: sourceURL, Provenance: []byte("admin")},
	)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}

	return &crawlrpc.IngestBatchMessage{BatchJson: data}
}

func authorizeIngestMessage(
	t *testing.T,
	server *exchangeServer,
	message *crawlrpc.IngestBatchMessage,
	name string,
) {
	t.Helper()
	message.LeaseId = leaseOneForSession(
		t,
		server.queue,
		name,
		"worker",
		testWorkerSessionID,
	)
	message.WorkerId = "worker"
	message.WorkerSessionId = testWorkerSessionID
	record, found := leaseRecordFor(t, server.queue, message.LeaseId)
	if !found {
		t.Fatal("authorized ingest lease not found")
	}
	order, err := yagocrawlcontract.UnmarshalCrawlOrder(record.OrderData)
	if err != nil {
		t.Fatalf("decode authorized ingest order: %v", err)
	}
	batch, err := yagocrawlcontract.UnmarshalIngestBatch(message.BatchJson)
	if err != nil {
		t.Fatalf("decode authorized ingest batch: %v", err)
	}
	batch.ProfileHandle = order.Profile.Handle
	message.BatchJson, err = yagocrawlcontract.MarshalIngestBatch(batch)
	if err != nil {
		t.Fatalf("encode authorized ingest batch: %v", err)
	}
	activateTestWorkerSession(t, server, "worker", testWorkerSessionID)
}

func TestAckOrderAcksLease(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "ack", "w1", testWorkerSessionID)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if _, err := server.AckOrder(
		context.Background(),
		&crawlrpc.OrderAck{
			LeaseId: leaseID, WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
		},
	); err != nil {
		t.Fatalf("ack order: %v", err)
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want 0 after ack", n)
	}
}

func TestAckOrderDefersLease(t *testing.T) {
	set := withClock(t)
	base := time.Unix(900, 0)
	set(base)
	queue := memQueue(t)
	leaseID := leaseOneForSession(t, queue, "nak", "w1", testWorkerSessionID)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if _, err := server.AckOrder(
		context.Background(),
		&crawlrpc.OrderAck{
			LeaseId: leaseID, Requeue: true,
			WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
		},
	); err != nil {
		t.Fatalf("nak order: %v", err)
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want 0 during nak backoff", n)
	}
	record, ok := leaseRecordFor(t, queue, leaseID)
	if !ok || record.WorkerID != "" ||
		record.ExpiresAtUnixNano != base.Add(negativeAcknowledgmentRetryDelay).UnixNano() {
		t.Fatalf("deferred lease = %#v/%v", record, ok)
	}
}

func TestAckOrderRejectsEmptyLease(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	_, err := server.AckOrder(context.Background(), &crawlrpc.OrderAck{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestAckOrderRejectsUnavailableLeaseDisposition(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	_, err := server.AckOrder(context.Background(), &crawlrpc.OrderAck{
		LeaseId: "missing", WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("error = %v, want FailedPrecondition", err)
	}
}

func TestAckOrderSurfacesInternalError(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOneForSession(t, fixture.queue, "boom", "w1", testWorkerSessionID)
	fixture.engine.deleteErrors[leaseBucket] = errors.New("delete failed")
	server := newExchangeServer(fixture.queue, make(chan crawlresults.IngestDelivery))
	_, err := server.AckOrder(context.Background(), &crawlrpc.OrderAck{
		LeaseId: leaseID, WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("error = %v, want Internal", err)
	}
}

func TestHeartbeatSettlesLeases(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "w1", testWorkerSessionID)
	if _, err := server.Heartbeat(
		context.Background(),
		&crawlrpc.WorkerHeartbeat{
			WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
		},
	); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
}

func TestHeartbeatRejectsEmptyWorkerID(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	_, err := server.Heartbeat(context.Background(), &crawlrpc.WorkerHeartbeat{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestHeartbeatSurfacesInternalError(t *testing.T) {
	fixture := scriptedQueue(t)
	server := newExchangeServer(fixture.queue, make(chan crawlresults.IngestDelivery))
	activateTestWorkerSession(t, server, "w1", testWorkerSessionID)
	leaseID := leaseOneForSession(t, fixture.queue, "heartbeat", "w1", testWorkerSessionID)
	fixture.engine.buckets[leaseBucket][leaseID] = []byte("{")
	_, err := server.Heartbeat(context.Background(), &crawlrpc.WorkerHeartbeat{
		WorkerId: "w1", WorkerSessionId: testWorkerSessionID,
		ActiveLeaseIds: []string{leaseID},
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("error = %v, want Internal", err)
	}
}

func TestIngestReceiverReceives(t *testing.T) {
	receiver := newIngestReceiver()
	go func() { receiver.out <- crawlresults.IngestDelivery{} }()
	select {
	case <-receiver.Receive():
	case <-time.After(time.Second):
		t.Fatal("no delivery received")
	}
}
