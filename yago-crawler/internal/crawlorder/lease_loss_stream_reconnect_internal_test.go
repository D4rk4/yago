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

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type reconnectingLeaseClient struct {
	mu              sync.Mutex
	order           *crawlrpc.CrawlOrderMessage
	registrations   []*crawlrpc.WorkerRegistration
	heartbeats      int
	omissionStarted chan struct{}
	releaseOmission chan struct{}
	omissionStart   sync.Once
}

func (c *reconnectingLeaseClient) StreamOrders(
	ctx context.Context,
	registration *crawlrpc.WorkerRegistration,
	_ ...grpc.CallOption,
) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error) {
	c.mu.Lock()
	c.registrations = append(c.registrations, registration)
	c.mu.Unlock()

	return &reconnectingLeaseStream{ctx: ctx, order: c.order}, nil
}

func (*reconnectingLeaseClient) AckOrder(
	context.Context,
	*crawlrpc.OrderAck,
	...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	return &crawlrpc.OrderAckResult{}, nil
}

func (c *reconnectingLeaseClient) Heartbeat(
	ctx context.Context,
	heartbeat *crawlrpc.WorkerHeartbeat,
	_ ...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	c.mu.Lock()
	c.heartbeats++
	call := c.heartbeats
	c.mu.Unlock()
	if call == 2 {
		c.omissionStart.Do(func() { close(c.omissionStarted) })
		select {
		case <-c.releaseOmission:
		case <-ctx.Done():
			return nil, fmt.Errorf("delayed omission: %w", ctx.Err())
		}

		return &crawlrpc.WorkerHeartbeatResult{LeaseTtlMilliseconds: 60_000}, nil
	}

	return &crawlrpc.WorkerHeartbeatResult{
		RenewedLeaseIds:      append([]string(nil), heartbeat.GetActiveLeaseIds()...),
		LeaseTtlMilliseconds: 60_000,
	}, nil
}

func (c *reconnectingLeaseClient) registrationSnapshot() []*crawlrpc.WorkerRegistration {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]*crawlrpc.WorkerRegistration(nil), c.registrations...)
}

type reconnectingLeaseStream struct {
	grpc.ClientStream
	ctx   context.Context
	order *crawlrpc.CrawlOrderMessage
	sent  bool
}

func (s *reconnectingLeaseStream) Recv() (*crawlrpc.CrawlOrderMessage, error) {
	if !s.sent {
		s.sent = true

		return s.order, nil
	}
	<-s.ctx.Done()

	return nil, io.EOF
}

func (s *reconnectingLeaseStream) Context() context.Context {
	return s.ctx
}

func TestOpenIdleOrderStreamReconnectsAndReplaysAfterLeaseOmission(t *testing.T) {
	restoreHeartbeat := orderHeartbeatInterval
	restoreRetry := orderStreamRetryWait
	restoreTimeout := orderHeartbeatRequestTimeout
	t.Cleanup(func() {
		orderHeartbeatInterval = restoreHeartbeat
		orderStreamRetryWait = restoreRetry
		orderHeartbeatRequestTimeout = restoreTimeout
	})
	orderHeartbeatInterval = 5 * time.Millisecond
	orderStreamRetryWait = time.Millisecond
	orderHeartbeatRequestTimeout = 250 * time.Millisecond
	order := yagocrawlcontract.CrawlOrder{
		Profile: yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{Name: "resume"}),
	}
	orderData, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatal(err)
	}
	client := &reconnectingLeaseClient{
		order: &crawlrpc.CrawlOrderMessage{
			OrderJson: orderData,
			LeaseId:   "lease-resume",
		},
		omissionStarted: make(chan struct{}),
		releaseOmission: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(t.Context())
	registry := crawllease.NewGrantRegistry(ctx, 1)
	receiver := NewGRPCOrderReceiver(
		ctx,
		client,
		"stable-worker",
		nil,
		WithWorkerLeaseSession("process-session", registry),
	)
	first := awaitOrder(t, receiver)
	firstGrant, confirmed := registry.Context(first.LeaseID)
	if !confirmed {
		t.Fatal("first delivered lease is not confirmed")
	}
	select {
	case <-client.omissionStarted:
	case <-time.After(time.Second):
		t.Fatal("periodic heartbeat did not reach the delayed omission")
	}
	if registrations := client.registrationSnapshot(); len(registrations) != 1 {
		t.Fatalf("order streams before omission = %d, want 1", len(registrations))
	}
	close(client.releaseOmission)
	select {
	case <-firstGrant.Done():
	case <-time.After(time.Second):
		t.Fatal("omitted lease did not cancel its active work")
	}
	if !errors.Is(context.Cause(firstGrant), crawllease.ErrLeaseLost) {
		t.Fatalf("first lease context cause = %v", context.Cause(firstGrant))
	}
	second := awaitOrder(t, receiver)
	if second.LeaseID != first.LeaseID || second.Order.Profile.Name != "resume" {
		t.Fatalf("replayed order = %+v, want the same resumable lease", second)
	}
	if !registry.Confirmed(second.LeaseID) {
		t.Fatal("replayed lease was not adopted and renewed")
	}
	registrations := client.registrationSnapshot()
	if len(registrations) < 2 {
		t.Fatalf("order streams after omission = %d, want reconnect", len(registrations))
	}
	for _, registration := range registrations[:2] {
		if registration.GetWorkerId() != "stable-worker" ||
			registration.GetWorkerSessionId() != "process-session" {
			t.Fatalf("reconnect registration = %+v", registration)
		}
	}
	cancel()
	drainUntilClosed(t, receiver)
}

type boundedHeartbeatClient struct {
	deadlineObserved chan struct{}
}

func (*boundedHeartbeatClient) StreamOrders(
	context.Context,
	*crawlrpc.WorkerRegistration,
	...grpc.CallOption,
) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error) {
	panic("unexpected order stream")
}

func (*boundedHeartbeatClient) AckOrder(
	context.Context,
	*crawlrpc.OrderAck,
	...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	panic("unexpected order acknowledgment")
}

func (c *boundedHeartbeatClient) Heartbeat(
	ctx context.Context,
	_ *crawlrpc.WorkerHeartbeat,
	_ ...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	if _, bounded := ctx.Deadline(); bounded {
		close(c.deadlineObserved)
	}
	<-ctx.Done()

	return nil, fmt.Errorf("blocked heartbeat: %w", ctx.Err())
}

func TestHeartbeatExchangeBoundsBlockingCall(t *testing.T) {
	restore := orderHeartbeatRequestTimeout
	t.Cleanup(func() { orderHeartbeatRequestTimeout = restore })
	orderHeartbeatRequestTimeout = 20 * time.Millisecond
	client := &boundedHeartbeatClient{deadlineObserved: make(chan struct{})}
	started := time.Now()
	_, err := (heartbeatDelivery{client: client, workerID: "worker"}).exchange(
		t.Context(),
		nil,
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("heartbeat error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("bounded heartbeat took %s", elapsed)
	}
	select {
	case <-client.deadlineObserved:
	default:
		t.Fatal("heartbeat client did not receive a bounded context")
	}
}
