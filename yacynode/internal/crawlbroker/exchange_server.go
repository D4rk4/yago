package crawlbroker

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yacynode/internal/crawlresults"
)

var errIngestDeferred = errors.New("ingest pipeline saturated")

type exchangeServer struct {
	crawlrpc.UnimplementedCrawlExchangeServer
	queue  *DurableOrderQueue
	ingest chan<- crawlresults.IngestDelivery
}

func newExchangeServer(
	queue *DurableOrderQueue,
	ingest chan<- crawlresults.IngestDelivery,
) *exchangeServer {
	return &exchangeServer{queue: queue, ingest: ingest}
}

func (s *exchangeServer) StreamOrders(
	_ *crawlrpc.WorkerRegistration,
	stream crawlrpc.CrawlExchange_StreamOrdersServer,
) error {
	ctx := stream.Context()
	for {
		data, err := s.queue.dequeue(ctx)
		if err != nil {
			return status.FromContextError(ctx.Err()).Err()
		}
		if err := stream.Send(&crawlrpc.CrawlOrderMessage{OrderJson: data}); err != nil {
			_ = s.queue.enqueue(context.WithoutCancel(ctx), data)

			return fmt.Errorf("send crawl order: %w", err)
		}
	}
}

func (s *exchangeServer) SubmitIngest(
	ctx context.Context,
	msg *crawlrpc.IngestBatchMessage,
) (*crawlrpc.IngestAck, error) {
	batch, err := yacycrawlcontract.UnmarshalIngestBatch(msg.GetBatchJson())
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
