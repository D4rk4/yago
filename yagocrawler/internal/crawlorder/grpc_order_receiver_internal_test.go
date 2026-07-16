package crawlorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	onStream func()

	mu             sync.Mutex
	acks           []*crawlrpc.OrderAck
	ackCalls       []*crawlrpc.OrderAck
	ackErrors      []error
	heartbeats     []string
	heartbeatCalls []*crawlrpc.WorkerHeartbeat
	beatCalls      int
	ackErr         error
	beatErr        error
	beatErrors     []error
	beatDirectives []*crawlrpc.CrawlControlDirective
	blockHeartbeat bool
}

func (f *fakeStreamer) StreamOrders(
	_ context.Context,
	_ *crawlrpc.WorkerRegistration,
	_ ...grpc.CallOption,
) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error) {
	if f.onStream != nil {
		f.onStream()
	}
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
	f.ackCalls = append(f.ackCalls, in)
	if len(f.ackErrors) > 0 {
		err := f.ackErrors[0]
		f.ackErrors = f.ackErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if f.ackErr != nil {
		return nil, f.ackErr
	}
	f.acks = append(f.acks, in)

	return &crawlrpc.OrderAckResult{}, nil
}

func (f *fakeStreamer) acknowledgementCalls() []*crawlrpc.OrderAck {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]*crawlrpc.OrderAck(nil), f.ackCalls...)
}

func (f *fakeStreamer) Heartbeat(
	ctx context.Context,
	in *crawlrpc.WorkerHeartbeat,
	_ ...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	f.mu.Lock()
	f.beatCalls++
	f.heartbeatCalls = append(f.heartbeatCalls, in)
	block := f.blockHeartbeat
	f.mu.Unlock()
	if block {
		<-ctx.Done()

		return nil, fmt.Errorf("blocked heartbeat: %w", ctx.Err())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.beatErrors) > 0 {
		err := f.beatErrors[0]
		f.beatErrors = f.beatErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if f.beatErr != nil {
		return nil, f.beatErr
	}
	f.heartbeats = append(f.heartbeats, in.GetWorkerId())

	return &crawlrpc.WorkerHeartbeatResult{Directives: f.beatDirectives}, nil
}

func TestGRPCOrderReceiverBoundsStartupHeartbeatBeforeStreaming(t *testing.T) {
	restore := orderStartupHeartbeatTimeout
	t.Cleanup(func() { orderStartupHeartbeatTimeout = restore })
	orderStartupHeartbeatTimeout = 25 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streamed := make(chan struct{}, 1)
	client := &fakeStreamer{
		ctx:            ctx,
		blockHeartbeat: true,
		onStream: func() {
			streamed <- struct{}{}
		},
	}
	started := time.Now()
	receiver := NewGRPCOrderReceiver(ctx, client, "worker-bounded", nil)
	select {
	case <-streamed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("order stream did not open after the bounded startup heartbeat")
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("startup heartbeat blocked order intake for %s", elapsed)
	}
	cancel()
	drainUntilClosed(t, receiver)
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

func (f *fakeStreamer) heartbeatRequests() []*crawlrpc.WorkerHeartbeat {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]*crawlrpc.WorkerHeartbeat(nil), f.heartbeatCalls...)
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

func TestGRPCOrderReceiverStopsBeforeStreamingWhenAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &fakeStreamer{
		ctx: ctx,
		onStream: func() {
			t.Error("cancelled receiver opened an order stream")
		},
	}

	receiver := NewGRPCOrderReceiver(ctx, client, "worker-cancelled", nil)
	if _, ok := <-receiver.Receive(); ok {
		t.Fatal("cancelled receiver output remained open")
	}
}

func TestGRPCOrderReceiverAppliesStartupPriorityBeforeStreaming(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bootstrapEnabled := true
	streamedBeforeControl := make(chan struct{}, 1)
	handler := &recordingControlHandler{}
	client := &fakeStreamer{
		ctx: ctx,
		attempts: []streamAttempt{{results: []recvResult{
			orderResult(t, "priority"),
		}}},
		beatDirectives: []*crawlrpc.CrawlControlDirective{{
			Kind: crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_AUTOMATIC_DISCOVERY_PRIORITY,
		}},
		onStream: func() {
			directives := handler.snapshot()
			if len(directives) == 0 ||
				directives[0].Kind != yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority ||
				directives[0].PrioritizeAutomaticDiscovery == bootstrapEnabled {
				streamedBeforeControl <- struct{}{}
			}
		},
	}

	receiver := NewGRPCOrderReceiver(
		ctx,
		client,
		"worker-priority",
		handler,
	)
	if got := awaitOrder(t, receiver).Order.Profile.Name; got != "priority" {
		t.Fatalf("order = %q, want priority", got)
	}
	if got := handler.snapshot()[0]; got.PrioritizeAutomaticDiscovery {
		t.Fatalf("startup priority = %+v, want persisted node-disabled policy", got)
	}
	select {
	case <-streamedBeforeControl:
		t.Fatal("order stream started before startup priority control was applied")
	default:
	}
}

func TestGRPCOrderReceiverPeriodicHeartbeatConvergesAfterStartupFailure(t *testing.T) {
	fastHeartbeat(t)
	ctx, cancel := context.WithCancel(context.Background())
	handler := &recordingControlHandler{}
	client := &fakeStreamer{
		ctx:        ctx,
		beatErrors: []error{errors.New("startup heartbeat unavailable")},
		beatDirectives: []*crawlrpc.CrawlControlDirective{{
			Kind: crawlrpc.CrawlControlKind_CRAWL_CONTROL_KIND_SET_AUTOMATIC_DISCOVERY_PRIORITY,
		}},
	}
	receiver := NewGRPCOrderReceiver(ctx, client, "worker-retry", handler)
	deadline := time.After(2 * time.Second)
	for len(handler.snapshot()) == 0 {
		select {
		case <-deadline:
			t.Fatal("periodic heartbeat did not converge after startup failure")
		case <-time.After(time.Millisecond):
		}
	}
	if got := handler.snapshot()[0]; got.Kind !=
		yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority ||
		got.PrioritizeAutomaticDiscovery {
		t.Fatalf("converged directive = %+v, want disabled automatic discovery priority", got)
	}
	cancel()
	drainUntilClosed(t, receiver)
}

func TestSettleLeaseReportsAckError(t *testing.T) {
	client := &fakeStreamer{
		ctx:    context.Background(),
		ackErr: status.Error(codes.InvalidArgument, "ack rejected"),
	}
	if err := settleLease(
		context.Background(),
		client,
		"lease-x",
		false,
	)(context.Background()); err == nil {
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

func TestPeriodicHeartbeatsLogsError(t *testing.T) {
	fastHeartbeat(t)
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{ctx: ctx, beatErr: errors.New("heartbeat rejected")}
	done := make(chan struct{})
	go func() {
		periodicHeartbeats(
			ctx,
			heartbeatDelivery{client: client, workerID: "worker-1"},
			orderHeartbeatInterval,
		)
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
		t.Fatal("periodicHeartbeats did not stop on cancel")
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

func TestGRPCOrderReceiverTerminatesUndecodableOrders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{
		ctx: ctx,
		attempts: []streamAttempt{{results: []recvResult{
			{msg: &crawlrpc.CrawlOrderMessage{
				OrderJson: []byte("not json"),
				LeaseId:   "lease-malformed",
			}},
			orderResult(t, "good"),
		}}},
	}

	receiver := NewGRPCOrderReceiver(ctx, client, "worker-1", nil)
	if got := awaitOrder(t, receiver).Order.Profile.Name; got != "good" {
		t.Fatalf("order = %q, want good", got)
	}
	acks := client.ackedLeases()
	if len(acks) != 1 || acks[0].GetLeaseId() != "lease-malformed" || acks[0].GetRequeue() {
		t.Fatalf("malformed order settlements = %+v", acks)
	}
	cancel()
	drainUntilClosed(t, receiver)
}

func TestGRPCOrderReceiverContinuesAfterMalformedSettlementFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamer{
		ctx:    ctx,
		ackErr: status.Error(codes.InvalidArgument, "settlement rejected"),
		attempts: []streamAttempt{{results: []recvResult{
			{msg: &crawlrpc.CrawlOrderMessage{
				OrderJson: []byte("not json"),
				LeaseId:   "lease-malformed",
			}},
			orderResult(t, "good"),
		}}},
	}

	receiver := NewGRPCOrderReceiver(ctx, client, "worker-1", nil)
	if got := awaitOrder(t, receiver).Order.Profile.Name; got != "good" {
		t.Fatalf("order = %q, want good", got)
	}
	if calls := client.acknowledgementCalls(); len(calls) != 1 ||
		calls[0].GetLeaseId() != "lease-malformed" {
		t.Fatalf("malformed order settlement calls = %+v", calls)
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
