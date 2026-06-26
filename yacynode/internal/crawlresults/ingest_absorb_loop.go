package crawlresults

import (
	"context"
	"log/slog"
)

const (
	msgIngestBatchAbsorbed = "ingest batch absorbed"
	msgIngestBatchDeferred = "ingest batch deferred"
	msgIngestAckFailed     = "ingest batch ack failed"
	msgIngestNakFailed     = "ingest batch nak failed"
)

func (c *IngestConsumer) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case delivery, ok := <-c.stream.Receive():
			if !ok {
				return
			}
			c.absorb(ctx, delivery)
		}
	}
}

func (c *IngestConsumer) absorb(ctx context.Context, delivery IngestDelivery) {
	batch := delivery.Batch

	urlReceipt, err := c.urls.Receive(ctx, batch.Metadata)
	if err != nil || urlReceipt.Busy {
		c.redeliver(ctx, delivery, batch.SourceURL, err)
		return
	}

	postingReceipt, err := c.postings.Receive(ctx, batch.Postings)
	if err != nil || postingReceipt.Busy {
		c.redeliver(ctx, delivery, batch.SourceURL, err)
		return
	}

	if err := delivery.Ack(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestAckFailed,
			slog.String("sourceUrl", batch.SourceURL), slog.Any("error", err))
		return
	}
	slog.DebugContext(ctx, msgIngestBatchAbsorbed,
		slog.String("sourceUrl", batch.SourceURL),
		slog.Int("metadata", len(batch.Metadata)),
		slog.Int("postings", len(batch.Postings)))
}

func (c *IngestConsumer) redeliver(
	ctx context.Context,
	delivery IngestDelivery,
	sourceURL string,
	cause error,
) {
	slog.WarnContext(ctx, msgIngestBatchDeferred,
		slog.String("sourceUrl", sourceURL), slog.Any("error", cause))
	if err := delivery.Nak(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestNakFailed,
			slog.String("sourceUrl", sourceURL), slog.Any("error", err))
	}
}
