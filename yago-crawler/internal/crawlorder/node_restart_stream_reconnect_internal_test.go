package crawlorder

import (
	"context"
	"sync"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type nodeRestartLeaseClient struct {
	mutex         sync.Mutex
	order         *crawlrpc.CrawlOrderMessage
	registrations []*crawlrpc.WorkerRegistration
	heartbeats    int
	sessionLost   chan struct{}
	lossOnce      sync.Once
}

func (c *nodeRestartLeaseClient) StreamOrders(
	ctx context.Context,
	registration *crawlrpc.WorkerRegistration,
	_ ...grpc.CallOption,
) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error) {
	c.mutex.Lock()
	c.registrations = append(c.registrations, registration)
	attempt := len(c.registrations)
	message := &crawlrpc.CrawlOrderMessage{
		OrderJson: append([]byte(nil), c.order.GetOrderJson()...),
		LeaseId:   c.order.GetLeaseId(),
	}
	c.mutex.Unlock()
	if attempt > 1 {
		message.Recovered = true
		message.RecoveredBatchEnd = true
		message.RecoveredLeaseIds = []string{message.GetLeaseId()}
		message.RecoveredSessionLeaseIds = []string{message.GetLeaseId()}
	}

	return &reconnectingLeaseStream{ctx: ctx, order: message}, nil
}

func (*nodeRestartLeaseClient) AckOrder(
	context.Context,
	*crawlrpc.OrderAck,
	...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	return &crawlrpc.OrderAckResult{}, nil
}

func (c *nodeRestartLeaseClient) Heartbeat(
	_ context.Context,
	heartbeat *crawlrpc.WorkerHeartbeat,
	_ ...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	c.mutex.Lock()
	c.heartbeats++
	call := c.heartbeats
	registrations := len(c.registrations)
	c.mutex.Unlock()
	if call == 2 {
		return nil, status.Error(codes.Unavailable, "node restarting")
	}
	if call > 2 && registrations == 1 {
		c.lossOnce.Do(func() { close(c.sessionLost) })

		return nil, status.Error(codes.FailedPrecondition, "crawl lease lost")
	}

	return &crawlrpc.WorkerHeartbeatResult{
		RenewedLeaseIds:      append([]string(nil), heartbeat.GetActiveLeaseIds()...),
		LeaseTtlMilliseconds: uint64(time.Minute / time.Millisecond),
	}, nil
}

func (c *nodeRestartLeaseClient) registrationSnapshot() []*crawlrpc.WorkerRegistration {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return append([]*crawlrpc.WorkerRegistration(nil), c.registrations...)
}

func TestNodeRestartReconnectsBlockedOrderDelivery(t *testing.T) {
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
		Profile: yagocrawlcontract.NewCrawlProfile(
			yagocrawlcontract.CrawlProfile{Name: "node-restart"},
		),
	}
	orderData, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatal(err)
	}
	client := &nodeRestartLeaseClient{
		order: &crawlrpc.CrawlOrderMessage{
			OrderJson: orderData,
			LeaseId:   "node-restart-lease",
		},
		sessionLost: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(t.Context())
	registry := crawllease.NewGrantRegistry(ctx, 1)
	receiver := NewGRPCOrderReceiver(
		ctx,
		client,
		"stable-worker",
		nil,
		WithWorkerLeaseSession("stable-session", registry),
	)
	select {
	case <-client.sessionLost:
	case <-time.After(time.Second):
		t.Fatal("node session loss was not observed")
	}
	deadline := time.Now().Add(time.Second)
	for len(client.registrationSnapshot()) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	registrations := client.registrationSnapshot()
	if len(registrations) < 2 {
		t.Fatalf(
			"order stream registrations after node restart = %d, want reconnect",
			len(registrations),
		)
	}
	delivery := awaitOrder(t, receiver)
	if delivery.LeaseID != "node-restart-lease" ||
		delivery.Order.Profile.Name != "node-restart" ||
		!registry.Confirmed(delivery.LeaseID) {
		t.Fatalf("recovered node-restart delivery = %+v", delivery)
	}
	for _, registration := range registrations[:2] {
		if registration.GetWorkerId() != "stable-worker" ||
			registration.GetWorkerSessionId() != "stable-session" {
			t.Fatalf("node-restart registration = %+v", registration)
		}
	}
	cancel()
	drainUntilClosed(t, receiver)
}
