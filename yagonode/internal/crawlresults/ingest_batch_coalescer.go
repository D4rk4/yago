package crawlresults

import (
	"context"
	"time"
)

const ingestMicroBatchMaximumWait = 2 * time.Millisecond

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
	if ctx.Err() != nil {
		return group
	}
	for len(group) < ingestMicroBatch {
		select {
		case delivery, ok := <-receive:
			if !ok {
				return group
			}
			group = append(group, delivery)
		default:
			return collectIngestMicroBatchUntil(
				ctx,
				receive,
				group,
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
		case <-deadline:
			return group
		}
	}

	return group
}
