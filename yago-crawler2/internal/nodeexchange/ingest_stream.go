package nodeexchange

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func (exchange *Exchange) StreamIngest(
	ctx context.Context,
) (grpc.BidiStreamingClient[crawlrpc.IngestBatchMessage, crawlrpc.IngestAck], error) {
	return exchange.client.StreamIngest(ctx)
}

func (exchange *Exchange) IngestMessage(
	leaseID string,
	submissionID string,
	batch yagocrawlcontract.IngestBatch,
) (*crawlrpc.IngestBatchMessage, error) {
	encoded, err := yagocrawlcontract.MarshalIngestBatch(batch)
	if err != nil {
		return nil, fmt.Errorf("encode crawl ingest batch: %w", err)
	}
	return &crawlrpc.IngestBatchMessage{
		BatchJson:       encoded,
		LeaseId:         leaseID,
		WorkerId:        exchange.workerID,
		WorkerSessionId: exchange.sessionID,
		SubmissionId:    submissionID,
	}, nil
}
