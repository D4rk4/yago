package crawlresults

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
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

	if c.documents != nil && hasDocument(batch.Document) {
		doc := documentFromIngest(batch.Document)
		documentReceipt, err := c.documents.Receive(ctx, []documentstore.Document{doc})
		if err != nil || documentReceipt.Busy {
			c.redeliver(ctx, delivery, batch.SourceURL, err)
			return
		}
		if c.index != nil {
			if err := c.index.Index(ctx, doc); err != nil {
				c.redeliver(ctx, delivery, batch.SourceURL, err)
				return
			}
		}
	}

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

	c.observer.ObserveAbsorbed(
		len(batch.Document.ExtractedText),
		len(batch.Metadata),
		len(batch.Postings),
	)
	if err := delivery.Ack(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestAckFailed,
			slog.String("sourceUrl", batch.SourceURL), slog.Any("error", err))
		return
	}
	slog.DebugContext(ctx, msgIngestBatchAbsorbed,
		slog.String("sourceUrl", batch.SourceURL),
		slog.Bool("document", hasDocument(batch.Document)),
		slog.Int("metadata", len(batch.Metadata)),
		slog.Int("postings", len(batch.Postings)))
}

func hasDocument(doc yagocrawlcontract.DocumentIngest) bool {
	return doc.NormalizedURL != "" || doc.CanonicalURL != "" || doc.ExtractedText != ""
}

func documentFromIngest(doc yagocrawlcontract.DocumentIngest) documentstore.Document {
	return documentstore.Document{
		CanonicalURL:        doc.CanonicalURL,
		NormalizedURL:       doc.NormalizedURL,
		Title:               doc.Title,
		Headings:            doc.Headings,
		ExtractedText:       doc.ExtractedText,
		RawContentReference: doc.RawContentReference,
		Language:            doc.Language,
		ContentType:         doc.ContentType,
		FetchStatus:         doc.FetchStatus,
		FetchedAt:           doc.FetchedAt,
		IndexedAt:           doc.IndexedAt,
		ContentHash:         doc.ContentHash,
		Outlinks:            doc.Outlinks,
		Inlinks:             anchorTextFromIngest(doc.Inlinks),
		Images:              imageMetadataFromIngest(doc.Images),
		Metadata:            doc.Metadata,
	}
}

func anchorTextFromIngest(in []yagocrawlcontract.AnchorText) []documentstore.AnchorText {
	out := make([]documentstore.AnchorText, 0, len(in))
	for _, anchor := range in {
		out = append(out, documentstore.AnchorText{URL: anchor.URL, Text: anchor.Text})
	}
	return out
}

func imageMetadataFromIngest(in []yagocrawlcontract.ImageMetadata) []documentstore.ImageMetadata {
	out := make([]documentstore.ImageMetadata, 0, len(in))
	for _, image := range in {
		out = append(out, documentstore.ImageMetadata{
			URL:     image.URL,
			AltText: image.AltText,
		})
	}
	return out
}

func (c *IngestConsumer) redeliver(
	ctx context.Context,
	delivery IngestDelivery,
	sourceURL string,
	cause error,
) {
	c.observer.ObserveDeferred()
	slog.WarnContext(ctx, msgIngestBatchDeferred,
		slog.String("sourceUrl", sourceURL), slog.Any("error", cause))
	if err := delivery.Nak(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestNakFailed,
			slog.String("sourceUrl", sourceURL), slog.Any("error", err))
	}
}
