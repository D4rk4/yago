package crawlorder

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

const (
	msgOrderDecodeFailed    = "crawl order decode failed"
	msgOrderStreamReconnect = "crawl order stream reconnecting"
	msgHeartbeatFailed      = "crawl worker heartbeat failed"

	DefaultOrderRetryWait = time.Second
	// DefaultHeartbeatInterval also bounds how long a control directive
	// (cancel/pause/resume/rate) waits before it reaches the worker, since
	// directives are drained on the heartbeat. It is kept short so an operator's
	// cancel takes visible effect within a few seconds rather than up to half a
	// minute; the heartbeat is a cheap unary call and more frequent lease renewal
	// only makes reclaim more reliable.
	DefaultHeartbeatInterval = 5 * time.Second
	DefaultAckTimeout        = 5 * time.Second
)

var (
	orderStreamRetryWait   = DefaultOrderRetryWait
	orderHeartbeatInterval = DefaultHeartbeatInterval
	orderAckTimeout        = DefaultAckTimeout
)

// OrderStreamer is the slice of the node's CrawlExchange client the receiver
// needs: a server-streaming subscription for leased crawl orders plus the calls
// that settle a lease and keep it alive.
type OrderStreamer interface {
	StreamOrders(
		ctx context.Context,
		in *crawlrpc.WorkerRegistration,
		opts ...grpc.CallOption,
	) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error)
	AckOrder(
		ctx context.Context,
		in *crawlrpc.OrderAck,
		opts ...grpc.CallOption,
	) (*crawlrpc.OrderAckResult, error)
	Heartbeat(
		ctx context.Context,
		in *crawlrpc.WorkerHeartbeat,
		opts ...grpc.CallOption,
	) (*crawlrpc.WorkerHeartbeatResult, error)
}

// GRPCOrderReceiver subscribes to the node's crawl order stream and republishes
// each order on a channel the crawl order consumer drains. The node leases each
// streamed order, so acknowledgement settles the lease: a successful run acks it
// away, a cancelled run naks it back to the queue, and a periodic heartbeat
// keeps a live worker's in-flight leases from being reclaimed.
type GRPCOrderReceiver struct {
	out chan CrawlOrderDelivery
}

func NewGRPCOrderReceiver(
	ctx context.Context,
	client OrderStreamer,
	workerID string,
	control ControlHandler,
) *GRPCOrderReceiver {
	out := make(chan CrawlOrderDelivery)
	go streamCrawlOrders(ctx, client, workerID, out, orderStreamRetryWait)
	go heartbeatOrders(ctx, client, workerID, orderHeartbeatInterval, control)

	return &GRPCOrderReceiver{out: out}
}

func (r *GRPCOrderReceiver) Receive() <-chan CrawlOrderDelivery {
	return r.out
}

func streamCrawlOrders(
	ctx context.Context,
	client OrderStreamer,
	workerID string,
	out chan<- CrawlOrderDelivery,
	retryWait time.Duration,
) {
	defer close(out)
	for {
		stream, err := client.StreamOrders(ctx, &crawlrpc.WorkerRegistration{WorkerId: workerID})
		if err != nil {
			slog.WarnContext(ctx, msgOrderStreamReconnect, slog.Any("error", err))
		} else {
			drainOrderStream(ctx, client, stream, out)
		}
		select {
		case <-time.After(retryWait):
		case <-ctx.Done():
			return
		}
	}
}

func heartbeatOrders(
	ctx context.Context,
	client OrderStreamer,
	workerID string,
	interval time.Duration,
	control ControlHandler,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := client.Heartbeat(ctx, &crawlrpc.WorkerHeartbeat{WorkerId: workerID})
			if err != nil {
				slog.WarnContext(ctx, msgHeartbeatFailed, slog.Any("error", err))

				continue
			}
			dispatchDirectives(ctx, control, result.GetDirectives())
		}
	}
}

func drainOrderStream(
	ctx context.Context,
	client OrderStreamer,
	stream grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage],
	out chan<- CrawlOrderDelivery,
) {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return
		}
		order, err := yagocrawlcontract.UnmarshalCrawlOrder(msg.GetOrderJson())
		if err != nil {
			slog.WarnContext(ctx, msgOrderDecodeFailed, slog.Any("error", err))

			continue
		}
		if !deliverOrder(ctx, client, out, order, msg.GetLeaseId()) {
			return
		}
	}
}

func deliverOrder(
	ctx context.Context,
	client OrderStreamer,
	out chan<- CrawlOrderDelivery,
	order yagocrawlcontract.CrawlOrder,
	leaseID string,
) bool {
	delivery := CrawlOrderDelivery{
		Order: order,
		Ack:   settleLease(client, leaseID, false),
		Nak:   settleLease(client, leaseID, true),
		Term:  settleLease(client, leaseID, false),
	}
	select {
	case out <- delivery:
		return true
	case <-ctx.Done():
		return false
	}
}

func settleLease(client OrderStreamer, leaseID string, requeue bool) func(context.Context) error {
	return func(ctx context.Context) error {
		callCtx, cancel := context.WithTimeout(ctx, orderAckTimeout)
		defer cancel()
		if _, err := client.AckOrder(
			callCtx,
			&crawlrpc.OrderAck{LeaseId: leaseID, Requeue: requeue},
		); err != nil {
			return fmt.Errorf("settle crawl order lease: %w", err)
		}

		return nil
	}
}
