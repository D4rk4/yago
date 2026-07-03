package crawlorder

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawlcontract/crawlrpc"
)

type recvResult struct {
	msg *crawlrpc.CrawlOrderMessage
	err error
}

type fakeOrderStream struct {
	grpc.ClientStream
	ctx     context.Context
	results []recvResult
	index   int
}

func (s *fakeOrderStream) Recv() (*crawlrpc.CrawlOrderMessage, error) {
	if s.index < len(s.results) {
		result := s.results[s.index]
		s.index++

		return result.msg, result.err
	}
	<-s.ctx.Done()

	return nil, io.EOF
}

func (s *fakeOrderStream) Context() context.Context { return s.ctx }

type streamAttempt struct {
	results []recvResult
	err     error
}

type fakeStreamer struct {
	ctx      context.Context
	attempts []streamAttempt
	index    int
}

func (f *fakeStreamer) StreamOrders(
	_ context.Context,
	_ *crawlrpc.WorkerRegistration,
	_ ...grpc.CallOption,
) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error) {
	if f.index < len(f.attempts) {
		attempt := f.attempts[f.index]
		f.index++
		if attempt.err != nil {
			return nil, attempt.err
		}

		return &fakeOrderStream{ctx: f.ctx, results: attempt.results}, nil
	}

	return &fakeOrderStream{ctx: f.ctx}, nil
}

func fastRetry(t *testing.T) {
	t.Helper()
	restore := orderStreamRetryWait
	t.Cleanup(func() { orderStreamRetryWait = restore })
	orderStreamRetryWait = time.Millisecond
}

func orderResult(t *testing.T, name string) recvResult {
	t.Helper()
	order := yacycrawlcontract.CrawlOrder{
		Profile: yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{Name: name}),
	}
	data, err := yacycrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}

	return recvResult{msg: &crawlrpc.CrawlOrderMessage{OrderJson: data}}
}

func TestGRPCOrderReceiverDeliversOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{
		ctx:      ctx,
		attempts: []streamAttempt{{results: []recvResult{orderResult(t, "docs")}}},
	}

	receiver := NewGRPCOrderReceiver(ctx, client, "worker-1")
	delivery := awaitOrder(t, receiver)
	if delivery.Order.Profile.Name != "docs" {
		t.Fatalf("order = %q, want docs", delivery.Order.Profile.Name)
	}
	for _, ack := range []func(context.Context) error{delivery.Ack, delivery.Nak, delivery.Term} {
		if err := ack(ctx); err != nil {
			t.Fatalf("acknowledgement returned %v, want nil", err)
		}
	}
	cancel()
	drainUntilClosed(t, receiver)
}

func TestGRPCOrderReceiverReconnectsAfterStreamError(t *testing.T) {
	fastRetry(t)
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{
		ctx: ctx,
		attempts: []streamAttempt{
			{err: errors.New("stream unavailable")},
			{results: []recvResult{orderResult(t, "retry")}},
		},
	}

	receiver := NewGRPCOrderReceiver(ctx, client, "worker-1")
	if got := awaitOrder(t, receiver).Order.Profile.Name; got != "retry" {
		t.Fatalf("order = %q, want retry", got)
	}
	cancel()
	drainUntilClosed(t, receiver)
}

func TestGRPCOrderReceiverSkipsUndecodableOrders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{
		ctx: ctx,
		attempts: []streamAttempt{{results: []recvResult{
			{msg: &crawlrpc.CrawlOrderMessage{OrderJson: []byte("not json")}},
			orderResult(t, "good"),
		}}},
	}

	receiver := NewGRPCOrderReceiver(ctx, client, "worker-1")
	if got := awaitOrder(t, receiver).Order.Profile.Name; got != "good" {
		t.Fatalf("order = %q, want good", got)
	}
	cancel()
	drainUntilClosed(t, receiver)
}

func awaitOrder(t *testing.T, receiver *GRPCOrderReceiver) CrawlOrderDelivery {
	t.Helper()
	select {
	case delivery := <-receiver.Receive():
		return delivery
	case <-time.After(2 * time.Second):
		t.Fatal("no order delivered")

		return CrawlOrderDelivery{}
	}
}

// drainUntilClosed waits for the receiver's background goroutine to exit so a
// test that mutates the package retry seam does not race the goroutine's read
// of it during cleanup.
func drainUntilClosed(t *testing.T, receiver *GRPCOrderReceiver) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-receiver.Receive():
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("receiver goroutine did not exit")
		}
	}
}

func TestDrainOrderStreamStopsOnRecvError(t *testing.T) {
	ctx := context.Background()
	stream := &fakeOrderStream{ctx: ctx, results: []recvResult{{err: errors.New("recv broken")}}}
	out := make(chan CrawlOrderDelivery)
	drainOrderStream(ctx, stream, out)
}

func TestDrainOrderStreamStopsWhenCancelledMidSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := &fakeOrderStream{ctx: ctx, results: []recvResult{orderResult(t, "unread")}}
	out := make(chan CrawlOrderDelivery)
	drainOrderStream(ctx, stream, out)
}

func TestDeliverOrderSendsWhenReadable(t *testing.T) {
	out := make(chan CrawlOrderDelivery, 1)
	if !deliverOrder(context.Background(), out, yacycrawlcontract.CrawlOrder{}) {
		t.Fatal("deliverOrder returned false, want true for a readable channel")
	}
	if len(out) != 1 {
		t.Fatal("delivery was not enqueued")
	}
}

func TestStreamCrawlOrdersStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan CrawlOrderDelivery)
	done := make(chan struct{})
	go func() {
		streamCrawlOrders(ctx, &fakeStreamer{ctx: ctx}, "worker-1", out)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamCrawlOrders did not stop on cancelled context")
	}
	if _, ok := <-out; ok {
		t.Fatal("expected the order channel to be closed")
	}
}
