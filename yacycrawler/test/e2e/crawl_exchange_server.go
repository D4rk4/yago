//go:build e2e

package e2e

import (
	"context"
	"net"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawlcontract/crawlrpc"
)

// crawlExchange is the host-side stand-in for a node's CrawlExchange service.
// It streams queued orders to the crawler and captures the ingest batches the
// crawler submits back, mirroring the durable node without pulling in the node
// image.
type crawlExchange struct {
	crawlrpc.UnimplementedCrawlExchangeServer
	orders   chan *crawlrpc.CrawlOrderMessage
	ingested chan yacycrawlcontract.IngestBatch
	acked    chan *crawlrpc.OrderAck
}

func (e *crawlExchange) StreamOrders(
	_ *crawlrpc.WorkerRegistration,
	stream crawlrpc.CrawlExchange_StreamOrdersServer,
) error {
	for {
		select {
		case msg := <-e.orders:
			if err := stream.Send(msg); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (e *crawlExchange) SubmitIngest(
	ctx context.Context,
	msg *crawlrpc.IngestBatchMessage,
) (*crawlrpc.IngestAck, error) {
	batch, err := yacycrawlcontract.UnmarshalIngestBatch(msg.GetBatchJson())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode ingest batch: %v", err)
	}
	select {
	case e.ingested <- batch:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	return &crawlrpc.IngestAck{}, nil
}

func (e *crawlExchange) AckOrder(
	ctx context.Context,
	req *crawlrpc.OrderAck,
) (*crawlrpc.OrderAckResult, error) {
	select {
	case e.acked <- req:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	return &crawlrpc.OrderAckResult{}, nil
}

func (e *crawlExchange) Heartbeat(
	context.Context,
	*crawlrpc.WorkerHeartbeat,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	return &crawlrpc.WorkerHeartbeatResult{}, nil
}

func (e *crawlExchange) enqueue(t *testing.T, order yacycrawlcontract.CrawlOrder) {
	t.Helper()
	data, err := yacycrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	e.orders <- &crawlrpc.CrawlOrderMessage{OrderJson: data, LeaseId: "e2e-lease"}
}

func (e *crawlExchange) awaitIngest(t *testing.T) yacycrawlcontract.IngestBatch {
	t.Helper()
	select {
	case batch := <-e.ingested:
		return batch
	case <-time.After(90 * time.Second):
		t.Fatal("crawler did not submit an ingest batch")

		return yacycrawlcontract.IngestBatch{}
	}
}

func startExchange(t *testing.T) (int, *crawlExchange) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen crawl exchange: %v", err)
	}
	exchange := &crawlExchange{
		orders:   make(chan *crawlrpc.CrawlOrderMessage, 16),
		ingested: make(chan yacycrawlcontract.IngestBatch, 16),
		acked:    make(chan *crawlrpc.OrderAck, 16),
	}
	// nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- host-side e2e stand-in on loopback; matches the node's insecure internal transport (ADR-0014).
	server := grpc.NewServer()
	crawlrpc.RegisterCrawlExchangeServer(server, exchange)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	return listener.Addr().(*net.TCPAddr).Port, exchange
}
