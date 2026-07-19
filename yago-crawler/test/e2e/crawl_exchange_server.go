//go:build e2e

package e2e

import (
	"context"
	"crypto/sha256"
	"net"
	"testing"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

// crawlExchange is the host-side stand-in for a node's CrawlExchange service.
// It streams queued orders to the crawler and captures the ingest batches the
// crawler submits back, mirroring the durable node without pulling in the node
// image.
type crawlExchange struct {
	crawlrpc.UnimplementedCrawlExchangeServer
	orders      chan *crawlrpc.CrawlOrderMessage
	ingested    chan yagocrawlcontract.IngestBatch
	acked       chan *crawlrpc.OrderAck
	urlDenylist yagocrawlcontract.CrawlURLDenylist
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
	batch, err := yagocrawlcontract.UnmarshalIngestBatch(msg.GetBatchJson())
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

	result := &crawlrpc.OrderAckResult{}
	if len(req.GetOrderIdentity()) != 0 && len(req.GetConfirmationToken()) == 0 {
		result.ConfirmationToken = make([]byte, sha256.Size)
	}

	return result, nil
}

func (e *crawlExchange) Heartbeat(
	_ context.Context,
	request *crawlrpc.WorkerHeartbeat,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	return &crawlrpc.WorkerHeartbeatResult{
		RenewedLeaseIds:      append([]string(nil), request.GetActiveLeaseIds()...),
		LeaseTtlMilliseconds: uint64(time.Minute / time.Millisecond),
		UrlDenylist: &crawlrpc.CrawlURLDenylist{
			Revision:  append([]byte(nil), e.urlDenylist.Revision...),
			ExactUrls: append([]string(nil), e.urlDenylist.ExactURLs...),
			Domains:   append([]string(nil), e.urlDenylist.Domains...),
		},
	}, nil
}

func (e *crawlExchange) LeaseFetchStarts(
	_ context.Context,
	request *crawlrpc.FetchStartLeaseRequest,
) (*crawlrpc.FetchStartLeaseDecision, error) {
	return &crawlrpc.FetchStartLeaseDecision{
		Granted:   true,
		Sequence:  request.GetSequence(),
		Permits:   request.GetMaximumPermits(),
		Unlimited: true,
	}, nil
}

func (e *crawlExchange) enqueue(t *testing.T, order yagocrawlcontract.CrawlOrder) {
	t.Helper()
	data, err := yagocrawlcontract.MarshalCrawlOrder(order)
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	e.orders <- &crawlrpc.CrawlOrderMessage{OrderJson: data, LeaseId: "e2e-lease"}
}

func (e *crawlExchange) awaitIngest(t *testing.T) yagocrawlcontract.IngestBatch {
	t.Helper()
	select {
	case batch := <-e.ingested:
		return batch
	case <-time.After(90 * time.Second):
		t.Fatal("crawler did not submit an ingest batch")

		return yagocrawlcontract.IngestBatch{}
	}
}

func startExchange(t *testing.T) (int, *crawlExchange) {
	t.Helper()
	urlDenylist, err := yagocrawlcontract.NewCrawlURLDenylist(nil, nil)
	if err != nil {
		t.Fatalf("create empty crawl URL denylist: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen crawl exchange: %v", err)
	}
	exchange := &crawlExchange{
		orders:      make(chan *crawlrpc.CrawlOrderMessage, 16),
		ingested:    make(chan yagocrawlcontract.IngestBatch, 16),
		acked:       make(chan *crawlrpc.OrderAck, 16),
		urlDenylist: urlDenylist,
	}
	// nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- host-side e2e stand-in on loopback; matches the node's insecure internal transport (ADR-0014).
	server := grpc.NewServer()
	crawlrpc.RegisterCrawlExchangeServer(server, exchange)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	return listener.Addr().(*net.TCPAddr).Port, exchange
}
