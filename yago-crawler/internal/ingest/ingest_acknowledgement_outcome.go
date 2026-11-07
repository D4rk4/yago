package ingest

import (
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
)

func acknowledgedIngestOutcome(batch IngestBatch, ack *crawlrpc.IngestAck) error {
	if batch.NotModified != nil && !ack.GetNotModifiedRecorded() {
		return fmt.Errorf("submit ingest batch %s: %w", batch.SourceURL, ErrNotModifiedUnrecorded)
	}
	if ack.GetRejected() {
		return &RejectedError{Rule: ack.GetRejectionRule()}
	}

	return nil
}
