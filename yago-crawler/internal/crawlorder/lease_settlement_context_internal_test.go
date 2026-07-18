package crawlorder

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

type deadlineOrderStreamer struct {
	calls int
}

func (*deadlineOrderStreamer) StreamOrders(
	context.Context,
	*crawlrpc.WorkerRegistration,
	...grpc.CallOption,
) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error) {
	panic("unexpected order stream")
}

func (s *deadlineOrderStreamer) AckOrder(
	ctx context.Context,
	_ *crawlrpc.OrderAck,
	_ ...grpc.CallOption,
) (*crawlrpc.OrderAckResult, error) {
	s.calls++
	<-ctx.Done()

	return nil, fmt.Errorf("acknowledgement context: %w", ctx.Err())
}

func (*deadlineOrderStreamer) Heartbeat(
	context.Context,
	*crawlrpc.WorkerHeartbeat,
	...grpc.CallOption,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	panic("unexpected heartbeat")
}

func TestLeaseSettlementStopsWhenContextExpiresDuringRPC(t *testing.T) {
	client := &deadlineOrderStreamer{}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	err := (leaseSettlementSession{
		client:  client,
		request: &crawlrpc.OrderAck{LeaseId: "lease-deadline"},
	}).retry(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("settlement error = %v, want deadline exceeded", err)
	}
	if client.calls != 1 {
		t.Fatalf("acknowledgement calls = %d, want 1", client.calls)
	}
}
