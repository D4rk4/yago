package crawlorder

import (
	"context"
	"log/slog"
	"time"

	grpc "google.golang.org/grpc"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawlcontract/crawlrpc"
)

const (
	msgOrderDecodeFailed    = "crawl order decode failed"
	msgOrderStreamReconnect = "crawl order stream reconnecting"

	DefaultOrderRetryWait = time.Second
)

var orderStreamRetryWait = DefaultOrderRetryWait

// OrderStreamer is the slice of the node's CrawlExchange client the receiver
// needs: a server-streaming subscription for claimed crawl orders.
type OrderStreamer interface {
	StreamOrders(
		ctx context.Context,
		in *crawlrpc.WorkerRegistration,
		opts ...grpc.CallOption,
	) (grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage], error)
}

// GRPCOrderReceiver subscribes to the node's crawl order stream and republishes
// each order on a channel the crawl order consumer drains. The node deletes an
// order once it is streamed, so acknowledgement is a crawler-side no-op.
type GRPCOrderReceiver struct {
	out chan CrawlOrderDelivery
}

func NewGRPCOrderReceiver(
	ctx context.Context,
	client OrderStreamer,
	workerID string,
) *GRPCOrderReceiver {
	out := make(chan CrawlOrderDelivery)
	go streamCrawlOrders(ctx, client, workerID, out)

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
) {
	defer close(out)
	for {
		stream, err := client.StreamOrders(ctx, &crawlrpc.WorkerRegistration{WorkerId: workerID})
		if err != nil {
			slog.WarnContext(ctx, msgOrderStreamReconnect, slog.Any("error", err))
		} else {
			drainOrderStream(ctx, stream, out)
		}
		select {
		case <-time.After(orderStreamRetryWait):
		case <-ctx.Done():
			return
		}
	}
}

func drainOrderStream(
	ctx context.Context,
	stream grpc.ServerStreamingClient[crawlrpc.CrawlOrderMessage],
	out chan<- CrawlOrderDelivery,
) {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return
		}
		order, err := yacycrawlcontract.UnmarshalCrawlOrder(msg.GetOrderJson())
		if err != nil {
			slog.WarnContext(ctx, msgOrderDecodeFailed, slog.Any("error", err))

			continue
		}
		if !deliverOrder(ctx, out, order) {
			return
		}
	}
}

func deliverOrder(
	ctx context.Context,
	out chan<- CrawlOrderDelivery,
	order yacycrawlcontract.CrawlOrder,
) bool {
	select {
	case out <- CrawlOrderDelivery{Order: order, Ack: noopAck, Nak: noopAck, Term: noopAck}:
		return true
	case <-ctx.Done():
		return false
	}
}

func noopAck(context.Context) error { return nil }
