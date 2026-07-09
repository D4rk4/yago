package crawlresults

import (
	"context"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
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
	message := delivery.Message
	switch {
	case len(message.Metadata) > 0:
		c.absorbMetadata(ctx, delivery, message)
	case len(message.Postings) > 0:
		c.absorbPostings(ctx, delivery, message)
	default:
		c.acknowledge(ctx, delivery, message.CanonicalURL, 0, 0)
	}
}

func (c *IngestConsumer) absorbMetadata(
	ctx context.Context,
	delivery IngestDelivery,
	message yacycrawlcontract.CrawledPageIndexMessage,
) {
	receipt, err := c.urls.Receive(ctx, message.Metadata)
	if err != nil || receipt.Busy {
		c.redeliver(ctx, delivery, message.CanonicalURL, err)
		return
	}
	c.acknowledge(ctx, delivery, message.CanonicalURL, len(message.Metadata), 0)
}

func (c *IngestConsumer) absorbPostings(
	ctx context.Context,
	delivery IngestDelivery,
	message yacycrawlcontract.CrawledPageIndexMessage,
) {
	receipt, err := c.postings.Receive(ctx, message.Postings)
	if receipt.TooLarge {
		c.redeliverTooLarge(ctx, delivery, message.CanonicalURL, len(message.Postings))
		return
	}
	if err != nil || receipt.Busy {
		c.redeliver(ctx, delivery, message.CanonicalURL, err)
		return
	}
	c.acknowledge(ctx, delivery, message.CanonicalURL, 0, len(message.Postings))
}

func (c *IngestConsumer) acknowledge(
	ctx context.Context,
	delivery IngestDelivery,
	canonicalURL string,
	metadata int,
	postings int,
) {
	if err := delivery.Ack(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestAckFailed,
			slog.String("url", canonicalURL), slog.Any("error", err))
		return
	}
	slog.DebugContext(ctx, msgIngestBatchAbsorbed,
		slog.String("url", canonicalURL),
		slog.Int("metadata", metadata),
		slog.Int("postings", postings))
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
