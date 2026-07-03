package ingest

import (
	"context"
	"fmt"
	"time"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawlcontract/crawlrpc"
)

type IngestBatch = yacycrawlcontract.IngestBatch

const DefaultIngestRetryWait = 100 * time.Millisecond

// IngestSubmitter is the slice of the node's CrawlExchange client the publisher
// needs: a unary submission that blocks until the node absorbs the batch.
type IngestSubmitter interface {
	SubmitIngest(
		ctx context.Context,
		in *crawlrpc.IngestBatchMessage,
		opts ...grpc.CallOption,
	) (*crawlrpc.IngestAck, error)
}

// GRPCIngestPublisher forwards ingest batches to the node over gRPC. The node
// reports a saturated pipeline with ResourceExhausted, on which the publisher
// retries so backpressure never drops a batch.
type GRPCIngestPublisher struct {
	client    IngestSubmitter
	retryWait time.Duration
}

func NewGRPCIngestPublisher(client IngestSubmitter) *GRPCIngestPublisher {
	return &GRPCIngestPublisher{client: client, retryWait: DefaultIngestRetryWait}
}

func (p *GRPCIngestPublisher) Publish(ctx context.Context, batch IngestBatch) error {
	data, _ := yacycrawlcontract.MarshalIngestBatch(batch)
	msg := &crawlrpc.IngestBatchMessage{BatchJson: data}
	for {
		if _, err := p.client.SubmitIngest(ctx, msg); err == nil {
			return nil
		} else if status.Code(err) != codes.ResourceExhausted {
			return fmt.Errorf("submit ingest batch %s: %w", batch.SourceURL, err)
		}
		select {
		case <-time.After(p.retryWait):
		case <-ctx.Done():
			return fmt.Errorf("submit ingest batch %s: %w", batch.SourceURL, ctx.Err())
		}
	}
}
