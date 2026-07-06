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
	msgIngestBatchRejected = "ingest batch rejected"
	msgIngestAckFailed     = "ingest batch ack failed"
	msgIngestNakFailed     = "ingest batch nak failed"
	msgRecrawlRecordFailed = "recrawl schedule record failed"
	msgIngestNearDuplicate = "ingest document near-duplicate collapsed"
	msgIngestLowQuality    = "ingest document rejected by quality gate"
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

	if reason := batchRejectionReason(batch); reason != "" {
		c.reject(ctx, delivery, reason)
		return
	}

	owned, err := c.owner.OwnsProfile(ctx, batch.ProfileHandle)
	if err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "ownership check", err)
		return
	}
	if !owned {
		c.reject(ctx, delivery, "unowned profile")
		return
	}

	if rule := c.qualityRejectionRule(batch); rule != "" {
		c.rejectLowQuality(ctx, delivery, rule)
		return
	}

	if deferred := c.storeDocument(ctx, delivery, batch); deferred {
		return
	}

	urlReceipt, err := c.urls.Receive(ctx, batch.Metadata)
	if err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "url metadata store", err)
		return
	}
	if urlReceipt.Busy {
		c.redeliver(ctx, delivery, batch.SourceURL, "url metadata at capacity", nil)
		return
	}

	postingReceipt, err := c.postings.Receive(ctx, batch.Postings)
	if err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "posting store", err)
		return
	}
	if postingReceipt.Busy {
		c.redeliver(ctx, delivery, batch.SourceURL, "posting store at capacity", nil)
		return
	}

	c.observer.ObserveAbsorbed(
		len(batch.Document.ExtractedText),
		len(batch.Metadata),
		len(batch.Postings),
	)
	c.recordFetch(ctx, batch)
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

// storeDocument persists and indexes the batch's document, if it carries one. It
// returns true when the batch was redelivered (a store error or capacity
// backpressure), so absorb stops before metadata and postings; a document store
// and its search index must stay in step.
func (c *IngestConsumer) storeDocument(
	ctx context.Context,
	delivery IngestDelivery,
	batch yagocrawlcontract.IngestBatch,
) bool {
	if c.documents == nil || !hasDocument(batch.Document) {
		return false
	}

	doc := documentFromIngest(batch.Document)
	if c.collapseNearDuplicate(ctx, doc) {
		return false
	}
	receipt, err := c.documents.Receive(ctx, []documentstore.Document{doc})
	if err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "document store", err)
		return true
	}
	if receipt.Busy {
		c.redeliver(ctx, delivery, batch.SourceURL, "document store at capacity", nil)
		return true
	}
	if c.index != nil {
		if err := c.index.Index(ctx, doc); err != nil {
			c.redeliver(ctx, delivery, batch.SourceURL, "search index", err)
			return true
		}
	}

	return false
}

// qualityRejectionRule names the quality rule the batch's document text
// violates, or "" when the batch may be absorbed (no gate, no document, or
// text worth indexing).
func (c *IngestConsumer) qualityRejectionRule(batch yagocrawlcontract.IngestBatch) string {
	if c.quality == nil || !hasDocument(batch.Document) ||
		batch.Document.ExtractedText == "" {
		return ""
	}

	return c.quality(batch.Document.ExtractedText)
}

// rejectLowQuality drops a batch whose page failed the content-quality gate:
// the document is not stored, its postings never reach the shared index, and
// the rejection is counted with its rule named.
func (c *IngestConsumer) rejectLowQuality(
	ctx context.Context,
	delivery IngestDelivery,
	rule string,
) {
	c.observer.ObserveLowQuality()
	slog.DebugContext(ctx, msgIngestLowQuality,
		slog.String("sourceUrl", delivery.Batch.SourceURL),
		slog.String("rule", rule))
	if err := delivery.Ack(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestAckFailed,
			slog.String("sourceUrl", delivery.Batch.SourceURL), slog.Any("error", err))
	}
}

// collapseNearDuplicate reports whether the document's text near-duplicates a
// recently stored page: the duplicate keeps its URL metadata and postings (the
// URL is real and its links count) but is not stored or indexed as another
// copy, so mirrors and session-id spellings collapse to one index entry.
func (c *IngestConsumer) collapseNearDuplicate(
	ctx context.Context,
	doc documentstore.Document,
) bool {
	if c.nearDup == nil {
		return false
	}
	key := doc.NormalizedURL
	if key == "" {
		key = doc.CanonicalURL
	}
	original, duplicate := c.nearDup.Observe(key, doc.ExtractedText)
	if !duplicate {
		return false
	}
	c.observer.ObserveDuplicate()
	slog.DebugContext(ctx, msgIngestNearDuplicate,
		slog.String("url", key),
		slog.String("duplicateOf", original))

	return true
}

// recordFetch feeds the recrawl schedule after a page batch is absorbed. It is
// best-effort: a failure is logged, never propagated, so it cannot fail an ingest
// that already stored its document, metadata, and postings. Only real page fetches
// (a document with a source URL and a fetch time) are recorded.
func (c *IngestConsumer) recordFetch(ctx context.Context, batch yagocrawlcontract.IngestBatch) {
	if batch.SourceURL == "" ||
		!hasDocument(batch.Document) ||
		batch.Document.FetchedAt.IsZero() {
		return
	}
	if err := c.recorder.RecordFetch(
		ctx,
		batch.SourceURL,
		batch.ProfileHandle,
		batch.Document.FetchedAt,
	); err != nil {
		slog.WarnContext(ctx, msgRecrawlRecordFailed,
			slog.String("sourceUrl", batch.SourceURL), slog.Any("error", err))
	}
}

// batchRejectionReason returns why a batch is malformed, or "" when it is well
// formed. A batch must name the source URL it came from, and any document it
// carries must have a URL to be stored under; a batch failing either cannot be
// attributed or indexed, so retrying it would only re-fail — it is dropped, not
// deferred.
func batchRejectionReason(batch yagocrawlcontract.IngestBatch) string {
	if batch.SourceURL == "" {
		return "missing source url"
	}
	if hasDocument(batch.Document) &&
		batch.Document.NormalizedURL == "" &&
		batch.Document.CanonicalURL == "" {
		return "document without url"
	}

	return ""
}

// reject drops a malformed batch: it records the rejection, logs the reason, and
// acks the delivery so the poison batch leaves the queue instead of being
// redelivered forever.
func (c *IngestConsumer) reject(ctx context.Context, delivery IngestDelivery, reason string) {
	c.observer.ObserveRejected()
	slog.WarnContext(ctx, msgIngestBatchRejected,
		slog.String("sourceUrl", delivery.Batch.SourceURL),
		slog.String("reason", reason))
	if err := delivery.Ack(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestAckFailed,
			slog.String("sourceUrl", delivery.Batch.SourceURL), slog.Any("error", err))
	}
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

// redeliver naks a batch so the stream hands it back later. A nil cause means
// plain capacity backpressure — an expected, self-clearing condition logged at
// debug so a busy vault cannot flood the operator; a non-nil cause is a real
// storage fault and is logged at warn with the error.
func (c *IngestConsumer) redeliver(
	ctx context.Context,
	delivery IngestDelivery,
	sourceURL string,
	reason string,
	cause error,
) {
	c.observer.ObserveDeferred()
	if cause != nil {
		slog.WarnContext(ctx, msgIngestBatchDeferred,
			slog.String("sourceUrl", sourceURL),
			slog.String("reason", reason),
			slog.Any("error", cause))
	} else {
		slog.DebugContext(ctx, msgIngestBatchDeferred,
			slog.String("sourceUrl", sourceURL),
			slog.String("reason", reason))
	}
	if err := delivery.Nak(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestNakFailed,
			slog.String("sourceUrl", sourceURL), slog.Any("error", err))
	}
}
