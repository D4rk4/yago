package crawlorder

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
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

	mu             sync.Mutex
	acks           []*crawlrpc.OrderAck
	heartbeats     []string
	beatCalls      int
	ackErr         error
	beatErr        error
	beatDirectives []*crawlrpc.CrawlControlDirective
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

func (f *fakeStreamer) AckOrder(
	_ context.Context,
	in *crawlrpc.OrderAck,
	_ ...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ackErr != nil {
		return nil, f.ackErr
	}
	f.acks = append(f.acks, in)

	return &crawlrpc.OrderAckResult{}, nil
}

func (f *fakeStreamer) Heartbeat(
	_ context.Context,
	in *crawlrpc.WorkerHeartbeat,
	_ ...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beatCalls++
	if f.beatErr != nil {
		return nil, f.beatErr
	}
	f.heartbeats = append(f.heartbeats, in.GetWorkerId())

	return &crawlrpc.WorkerHeartbeatResult{Directives: f.beatDirectives}, nil
}

func (f *fakeStreamer) ackedLeases() []*crawlrpc.OrderAck {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]*crawlrpc.OrderAck(nil), f.acks...)
}

func (f *fakeStreamer) beats() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.heartbeats...)
}

func (f *fakeStreamer) beatCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.beatCalls
}

func fastRetry(t *testing.T) {
	t.Helper()
	restore := orderStreamRetryWait
	t.Cleanup(func() { orderStreamRetryWait = restore })
	orderStreamRetryWait = time.Millisecond
}

func fastHeartbeat(t *testing.T) {
	t.Helper()
	restore := orderHeartbeatInterval
	t.Cleanup(func() { orderHeartbeatInterval = restore })
	orderHeartbeatInterval = time.Millisecond
}

func orderResult(t *testing.T, name string) recvResult {
	t.Helper()
	order := yagocrawlcontract.CrawlOrder{
		Profile: yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{Name: name}),
	}
	data, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}

	return recvResult{msg: &crawlrpc.CrawlOrderMessage{OrderJson: data, LeaseId: "lease-" + name}}
}

func TestGRPCOrderReceiverDeliversOrderAndSettlesLease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{
		ctx:      ctx,
		attempts: []streamAttempt{{results: []recvResult{orderResult(t, "docs")}}},
	}

	receiver := NewGRPCOrderReceiver(ctx, client, "worker-1", nil)
	delivery := awaitOrder(t, receiver)
	if delivery.Order.Profile.Name != "docs" {
		t.Fatalf("order = %q, want docs", delivery.Order.Profile.Name)
	}
	for _, ack := range []func(context.Context) error{delivery.Ack, delivery.Nak, delivery.Term} {
		if err := ack(ctx); err != nil {
			t.Fatalf("acknowledgement returned %v, want nil", err)
		}
	}
	acks := client.ackedLeases()
	if len(acks) != 3 {
		t.Fatalf("recorded %d acks, want 3", len(acks))
	}
	if acks[0].GetLeaseId() != "lease-docs" || acks[0].GetRequeue() {
		t.Fatalf("ack = %+v, want lease-docs without requeue", acks[0])
	}
	if !acks[1].GetRequeue() {
		t.Fatalf("nak = %+v, want requeue", acks[1])
	}
	if acks[2].GetRequeue() {
		t.Fatalf("term = %+v, want no requeue", acks[2])
	}
	cancel()
	drainUntilClosed(t, receiver)
}

func TestSettleLeaseReportsAckError(t *testing.T) {
	client := &fakeStreamer{ctx: context.Background(), ackErr: errors.New("ack rejected")}
	if err := settleLease(client, "lease-x", false)(context.Background()); err == nil {
		t.Fatal("expected settleLease to surface the ack error")
	}
}

func TestGRPCOrderReceiverHeartbeatsWorker(t *testing.T) {
	fastHeartbeat(t)
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{ctx: ctx}

	receiver := NewGRPCOrderReceiver(ctx, client, "worker-9", nil)
	deadline := time.After(2 * time.Second)
	for len(client.beats()) == 0 {
		select {
		case <-deadline:
			t.Fatal("no heartbeat observed")
		case <-time.After(time.Millisecond):
		}
	}
	if got := client.beats()[0]; got != "worker-9" {
		t.Fatalf("heartbeat worker = %q, want worker-9", got)
	}
	cancel()
	drainUntilClosed(t, receiver)
}

func TestHeartbeatOrdersLogsError(t *testing.T) {
	fastHeartbeat(t)
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{ctx: ctx, beatErr: errors.New("heartbeat rejected")}
	done := make(chan struct{})
	go func() {
		heartbeatOrders(ctx, client, "worker-1", orderHeartbeatInterval, nil)
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for client.beatCallCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("heartbeat was never attempted")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeatOrders did not stop on cancel")
	}
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

	receiver := NewGRPCOrderReceiver(ctx, client, "worker-1", nil)
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

	receiver := NewGRPCOrderReceiver(ctx, client, "worker-1", nil)
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
	drainOrderStream(ctx, &fakeStreamer{ctx: ctx}, stream, out)
}

func TestDrainOrderStreamStopsWhenCancelledMidSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := &fakeOrderStream{ctx: ctx, results: []recvResult{orderResult(t, "unread")}}
	out := make(chan CrawlOrderDelivery)
	drainOrderStream(ctx, &fakeStreamer{ctx: ctx}, stream, out)
}

func TestDeliverOrderSendsWhenReadable(t *testing.T) {
	client := &fakeStreamer{ctx: context.Background()}
	out := make(chan CrawlOrderDelivery, 1)
	if !deliverOrder(context.Background(), client, out, yagocrawlcontract.CrawlOrder{}, "lease-x") {
		t.Fatal("deliverOrder returned false, want true for a readable channel")
	}
	if len(out) != 1 {
		t.Fatal("delivery was not enqueued")
	}
	if err := (<-out).Ack(context.Background()); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if acks := client.ackedLeases(); len(acks) != 1 || acks[0].GetLeaseId() != "lease-x" {
		t.Fatalf("acks = %+v, want one lease-x ack", acks)
	}
}

func TestStreamCrawlOrdersStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan CrawlOrderDelivery)
	done := make(chan struct{})
	go func() {
		streamCrawlOrders(ctx, &fakeStreamer{ctx: ctx}, "worker-1", out, orderStreamRetryWait)
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
