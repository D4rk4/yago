package crawlbroker

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yacynode/internal/crawlresults"
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
	order, err := yacycrawlcontract.UnmarshalCrawlOrder(stream.sent[0].GetOrderJson())
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if order.Profile.Name != "stream" {
		t.Fatalf("order = %q, want stream", order.Profile.Name)
	}
}

func TestStreamOrdersRequeuesOnSendError(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	if err := queue.Publish(context.Background(), testOrder("requeue")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	stream := &fakeOrderStream{ctx: context.Background(), sendErr: errors.New("stream broken")}
	if err := server.StreamOrders(&crawlrpc.WorkerRegistration{}, stream); err == nil {
		t.Fatal("expected send error")
	}

	data, _, err := queue.pop(context.Background())
	if err != nil {
		t.Fatalf("pop: %v", err)
	}
	if data == nil {
		t.Fatal("order was not requeued after send failure")
	}
}

func TestStreamOrdersReturnsWhenContextCancelled(t *testing.T) {
	queue := memQueue(t)
	server := newExchangeServer(queue, make(chan crawlresults.IngestDelivery))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.StreamOrders(
		&crawlrpc.WorkerRegistration{},
		&fakeOrderStream{ctx: ctx},
	); err == nil {
		t.Fatal("expected cancellation error")
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
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("error = %v, want ResourceExhausted", err)
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
	data, err := yacycrawlcontract.MarshalIngestBatch(
		yacycrawlcontract.IngestBatch{SourceURL: sourceURL},
	)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}

	return &crawlrpc.IngestBatchMessage{BatchJson: data}
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
