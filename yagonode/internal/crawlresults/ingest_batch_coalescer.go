package crawlresults

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	ingestMicroBatchMaximumWait      = 10 * time.Millisecond
	ingestMicroBatchMaximumJSONBytes = 64 << 20
	ingestMicroBatchJSONStopAt       = ingestMicroBatchMaximumJSONBytes -
		yagocrawlcontract.MaximumIngestBatchBytes
)

type ingestBatchDeadline func(time.Duration) <-chan time.Time

func (c *IngestConsumer) drainPending(
	ctx context.Context,
	first IngestDelivery,
) []IngestDelivery {
	return collectIngestMicroBatch(
		ctx,
		c.stream.Receive(),
		first,
		time.After,
	)
}

func collectIngestMicroBatch(
	ctx context.Context,
	receive <-chan IngestDelivery,
	first IngestDelivery,
	deadline ingestBatchDeadline,
) []IngestDelivery {
	group := make([]IngestDelivery, 1, ingestMicroBatch)
	group[0] = first
	groupJSONBytes := first.BatchJSONSize
	if ctx.Err() != nil || groupJSONBytes >= ingestMicroBatchJSONStopAt {
		return group
	}
	for len(group) < ingestMicroBatch {
		select {
		case delivery, ok := <-receive:
			if !ok {
				return group
			}
			group = append(group, delivery)
			groupJSONBytes += delivery.BatchJSONSize
			if groupJSONBytes >= ingestMicroBatchJSONStopAt {
				return group
			}
		default:
			return collectIngestMicroBatchUntil(
				ctx,
				receive,
				group,
				groupJSONBytes,
				deadline(ingestMicroBatchMaximumWait),
			)
		}
	}

	return group
}

func collectIngestMicroBatchUntil(
	ctx context.Context,
	receive <-chan IngestDelivery,
	group []IngestDelivery,
	groupJSONBytes int,
	deadline <-chan time.Time,
) []IngestDelivery {
	for len(group) < ingestMicroBatch {
		select {
		case <-ctx.Done():
			return group
		case delivery, ok := <-receive:
			if !ok {
				return group
			}
			group = append(group, delivery)
			groupJSONBytes += delivery.BatchJSONSize
			if groupJSONBytes >= ingestMicroBatchJSONStopAt {
				return group
			}
		case <-deadline:
			return group
		}
	}

	return group
}
