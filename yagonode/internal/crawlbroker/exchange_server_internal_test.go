package crawlbroker

import (
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
	_ = server.StreamOrders(&crawlrpc.WorkerRegistration{WorkerId: "w1"}, stream)

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
		&crawlrpc.WorkerRegistration{WorkerId: "w1"},
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
		&crawlrpc.WorkerRegistration{WorkerId: "w1"},
		&fakeOrderStream{ctx: ctx},
	); err == nil {
		t.Fatal("expected cancellation error")
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
	_ = server.StreamOrders(&crawlrpc.WorkerRegistration{WorkerId: "w1"}, stream)
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
	server := newExchangeServer(memQueue(t), out)
	go func() {
		delivery := <-out
		_ = delivery.Ack(context.Background())
	}()

	msg := ingestMessage(t, "https://example.org/a")
	if _, err := server.SubmitIngest(context.Background(), msg); err != nil {
		t.Fatalf("submit ingest: %v", err)
	}
}

func TestSubmitIngestReportsSaturation(t *testing.T) {
	out := make(chan crawlresults.IngestDelivery)
	server := newExchangeServer(memQueue(t), out)
	go func() {
		delivery := <-out
		_ = delivery.Nak(context.Background())
	}()

	_, err := server.SubmitIngest(context.Background(), ingestMessage(t, "https://example.org/b"))
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

func TestSubmitIngestReturnsWhenContextCancelledBeforeDelivery(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := server.SubmitIngest(ctx, ingestMessage(t, "https://example.org/c")); err == nil {
		t.Fatal("expected cancellation error before delivery")
	}
}

func TestSubmitIngestReturnsWhenContextCancelledAwaitingResult(t *testing.T) {
	out := make(chan crawlresults.IngestDelivery)
	server := newExchangeServer(memQueue(t), out)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-out
		cancel()
	}()
	if _, err := server.SubmitIngest(ctx, ingestMessage(t, "https://example.org/d")); err == nil {
		t.Fatal("expected cancellation error awaiting result")
	}
}

func ingestMessage(t *testing.T, sourceURL string) *crawlrpc.IngestBatchMessage {
	t.Helper()
	data, err := yagocrawlcontract.MarshalIngestBatch(
		yagocrawlcontract.IngestBatch{SourceURL: sourceURL},
	)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}

	return &crawlrpc.IngestBatchMessage{BatchJson: data}
}

func TestAckOrderAcksLease(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "ack", "w1")
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if _, err := server.AckOrder(
		context.Background(),
		&crawlrpc.OrderAck{LeaseId: leaseID},
	); err != nil {
		t.Fatalf("ack order: %v", err)
	}
	if n := pendingCount(t, queue); n != 0 {
		t.Fatalf("pending = %d, want 0 after ack", n)
	}
}

func TestAckOrderRequeuesLease(t *testing.T) {
	queue := memQueue(t)
	leaseID := leaseOne(t, queue, "nak", "w1")
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if _, err := server.AckOrder(
		context.Background(),
		&crawlrpc.OrderAck{LeaseId: leaseID, Requeue: true},
	); err != nil {
		t.Fatalf("nak order: %v", err)
	}
	if n := pendingCount(t, queue); n != 1 {
		t.Fatalf("pending = %d, want 1 after nak", n)
	}
}

func TestAckOrderRejectsEmptyLease(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	_, err := server.AckOrder(context.Background(), &crawlrpc.OrderAck{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestAckOrderSurfacesInternalError(t *testing.T) {
	fixture := scriptedQueue(t)
	leaseID := leaseOne(t, fixture.queue, "boom", "w1")
	fixture.engine.deleteErrors[leaseBucket] = errors.New("delete failed")
	server := newExchangeServer(fixture.queue, make(chan crawlresults.IngestDelivery))
	_, err := server.AckOrder(context.Background(), &crawlrpc.OrderAck{LeaseId: leaseID})
	if status.Code(err) != codes.Internal {
		t.Fatalf("error = %v, want Internal", err)
	}
}

func TestHeartbeatSettlesLeases(t *testing.T) {
	server := newExchangeServer(memQueue(t), make(chan crawlresults.IngestDelivery))
	if _, err := server.Heartbeat(
		context.Background(),
		&crawlrpc.WorkerHeartbeat{WorkerId: "w1"},
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
	fixture.engine.scanErrors[leaseBucket] = errors.New("scan failed")
	server := newExchangeServer(fixture.queue, make(chan crawlresults.IngestDelivery))
	_, err := server.Heartbeat(context.Background(), &crawlrpc.WorkerHeartbeat{WorkerId: "w1"})
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
