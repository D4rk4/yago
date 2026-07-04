package crawlbroker

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/crawlresults"
)

var errIngestDeferred = errors.New("ingest pipeline saturated")

type exchangeServer struct {
	crawlrpc.UnimplementedCrawlExchangeServer
	queue    *DurableOrderQueue
	ingest   chan<- crawlresults.IngestDelivery
	progress ProgressSink
}

func newExchangeServer(
	queue *DurableOrderQueue,
	ingest chan<- crawlresults.IngestDelivery,
) *exchangeServer {
	return &exchangeServer{queue: queue, ingest: ingest, progress: noopProgressSink{}}
}

func (s *exchangeServer) StreamOrders(
	reg *crawlrpc.WorkerRegistration,
	stream crawlrpc.CrawlExchange_StreamOrdersServer,
) error {
	ctx := stream.Context()
	workerID := reg.GetWorkerId()
	for {
		data, leaseID, err := s.queue.leaseNext(ctx, workerID)
		if err != nil {
			return status.FromContextError(ctx.Err()).Err()
		}
		if err := stream.Send(
			&crawlrpc.CrawlOrderMessage{OrderJson: data, LeaseId: leaseID},
		); err != nil {
			_ = s.queue.requeueLease(context.WithoutCancel(ctx), leaseID)

			return fmt.Errorf("send crawl order: %w", err)
		}
	}
}

func (s *exchangeServer) AckOrder(
	ctx context.Context,
	req *crawlrpc.OrderAck,
) (*crawlrpc.OrderAckResult, error) {
	leaseID := req.GetLeaseId()
	if leaseID == "" {
		return nil, status.Error(codes.InvalidArgument, "empty lease id")
	}
	settle := s.queue.ackLease
	if req.GetRequeue() {
		settle = s.queue.requeueLease
	}
	if err := settle(ctx, leaseID); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &crawlrpc.OrderAckResult{}, nil
}

func (s *exchangeServer) Heartbeat(
	ctx context.Context,
	req *crawlrpc.WorkerHeartbeat,
) (*crawlrpc.WorkerHeartbeatResult, error) {
	if err := s.queue.heartbeat(ctx, req.GetWorkerId()); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &crawlrpc.WorkerHeartbeatResult{}, nil
}

func (s *exchangeServer) ReportProgress(
	ctx context.Context,
	report *crawlrpc.CrawlProgressReport,
) (*crawlrpc.CrawlProgressAck, error) {
	s.progress.Record(ctx, progressFromReport(report))

	return &crawlrpc.CrawlProgressAck{}, nil
}

func (s *exchangeServer) SubmitIngest(
	ctx context.Context,
	msg *crawlrpc.IngestBatchMessage,
) (*crawlrpc.IngestAck, error) {
	batch, err := yagocrawlcontract.UnmarshalIngestBatch(msg.GetBatchJson())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode ingest batch: %v", err)
	}

	result := make(chan error, 1)
	delivery := crawlresults.IngestDelivery{
		Batch: batch,
		Ack:   func(context.Context) error { result <- nil; return nil },
		Nak:   func(context.Context) error { result <- errIngestDeferred; return nil },
	}
	select {
	case s.ingest <- delivery:
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	select {
	case absorbErr := <-result:
		if absorbErr != nil {
			return nil, status.Error(codes.ResourceExhausted, absorbErr.Error())
		}

		return &crawlrpc.IngestAck{}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}
