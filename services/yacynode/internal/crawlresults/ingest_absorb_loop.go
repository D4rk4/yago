package crawlresults

import (
	"context"
	"log/slog"
)

const (
	msgIngestBatchAbsorbed = "ingest batch absorbed"
	msgIngestBatchDeferred = "ingest batch deferred"
	msgIngestBatchTooLarge = "ingest batch exceeds posting batch cap, deferred until operator intervenes"
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
		c.redeliver(ctx, delivery, batch.CanonicalURL, err)
		return
	}

	postingReceipt, err := c.postings.Receive(ctx, batch.Postings)
	if postingReceipt.TooLarge {
		c.redeliverTooLarge(ctx, delivery, batch.CanonicalURL, len(batch.Postings))
		return
	}
	if err != nil || postingReceipt.Busy {
		c.redeliver(ctx, delivery, batch.CanonicalURL, err)
		return
	}

	if err := delivery.Ack(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestAckFailed,
			slog.String("url", batch.CanonicalURL), slog.Any("error", err))
		return
	}
	slog.DebugContext(ctx, msgIngestBatchAbsorbed,
		slog.String("url", batch.CanonicalURL),
		slog.Int("metadata", len(batch.Metadata)),
		slog.Int("postings", len(batch.Postings)))
}

func (c *IngestConsumer) redeliver(
	ctx context.Context,
	delivery IngestDelivery,
	canonicalURL string,
	cause error,
) {
	slog.WarnContext(ctx, msgIngestBatchDeferred,
		slog.String("url", canonicalURL), slog.Any("error", cause))
	if err := delivery.Nak(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestNakFailed,
			slog.String("url", canonicalURL), slog.Any("error", err))
	}
}

func (c *IngestConsumer) redeliverTooLarge(
	ctx context.Context,
	delivery IngestDelivery,
	canonicalURL string,
	postingCount int,
) {
	slog.ErrorContext(ctx, msgIngestBatchTooLarge,
		slog.String("url", canonicalURL), slog.Int("postings", postingCount))
	if err := delivery.Nak(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestNakFailed,
			slog.String("url", canonicalURL), slog.Any("error", err))
	}
}
