package crawlresults

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const (
	msgIngestBatchAbsorbed = "ingest batch absorbed"
	msgIngestBatchDeferred = "ingest batch deferred"
	msgIngestBatchRejected = "ingest batch rejected"
	msgIngestAckFailed     = "ingest batch ack failed"
	msgIngestNakFailed     = "ingest batch nak failed"
	msgRecrawlRecordFailed = "recrawl schedule record failed"
	msgIngestLowQuality    = "ingest document rejected by quality gate"
	msgIngestRemovalPurged = "ingest dead page purged"
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
			c.absorbGroup(ctx, c.drainPending(delivery))
		}
	}
}

func (c *IngestConsumer) absorb(ctx context.Context, delivery IngestDelivery) {
	if delivery.Batch.Removed {
		c.absorbRemoval(ctx, delivery)

		return
	}
	if !c.passesGates(ctx, delivery) {
		return
	}
	current := c.beginObservations(ctx, []IngestDelivery{delivery})
	if len(current) == 0 {
		return
	}
	delivery = current[0]
	if deferred := c.storeDocument(ctx, delivery, delivery.Batch); deferred {
		return
	}
	c.absorbTail(ctx, delivery)
}

// absorbRemoval purges a dead-page tombstone (ADR-0034): a Removed batch names a
// URL a recrawl found permanently gone (404/410) and carries no document, so the
// document-store and content-quality gates are bypassed. Profile ownership is
// still enforced — a foreign crawler must not be able to delete arbitrary URLs —
// then the URL's postings and metadata are dropped idempotently and the delivery
// is acked. A purge failure redelivers so the tombstone is retried; a malformed
// source URL is a poison batch and is dropped.
func (c *IngestConsumer) absorbRemoval(ctx context.Context, delivery IngestDelivery) {
	if !c.passesGates(ctx, delivery) {
		return
	}
	current := c.beginObservations(ctx, []IngestDelivery{delivery})
	if len(current) == 0 {
		return
	}
	delivery = current[0]
	c.purgeRemoval(ctx, delivery)
}

func (c *IngestConsumer) purgeRemoval(ctx context.Context, delivery IngestDelivery) {
	batch := delivery.Batch
	hash, err := c.hashURL(batch.SourceURL)
	if err != nil {
		c.reject(ctx, delivery, "hash source url")

		return
	}
	if c.clearOutboundAnchors(ctx, delivery) {
		return
	}
	if err := c.deleteDocumentCluster(ctx, batch.SourceURL); err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "content cluster removal", err)

		return
	}
	if err := c.purger.Purge(ctx, []yagomodel.Hash{hash.Hash()}); err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "purge removal", err)

		return
	}
	if !c.completeObservations(ctx, []IngestDelivery{delivery}) {
		return
	}
	if err := delivery.Ack(ctx); err != nil {
		slog.WarnContext(ctx, msgIngestAckFailed,
			slog.String("sourceUrl", batch.SourceURL), slog.Any("error", err))

		return
	}
	slog.DebugContext(ctx, msgIngestRemovalPurged, slog.String("sourceUrl", batch.SourceURL))
}

// passesGates runs the per-delivery admission gates — wire validity, profile
// ownership, and the content-quality rule — handling rejection and redelivery
// itself; it reports whether the delivery may proceed to storage.
func (c *IngestConsumer) passesGates(ctx context.Context, delivery IngestDelivery) bool {
	batch := delivery.Batch
	if reason := batchRejectionReason(batch); reason != "" {
		c.reject(ctx, delivery, reason)

		return false
	}
	owned, err := c.owner.OwnsProfile(ctx, batch.ProfileHandle)
	if err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "ownership check", err)

		return false
	}
	if !owned {
		c.reject(ctx, delivery, "unowned profile")

		return false
	}
	if rule := c.qualityRejectionRule(batch); rule != "" {
		c.rejectLowQuality(ctx, delivery, rule)

		return false
	}

	return true
}

// absorbTail persists the batch's URL metadata and postings and acknowledges
// the delivery; it runs after the document (if any) is stored and indexed.
func (c *IngestConsumer) absorbTail(ctx context.Context, delivery IngestDelivery) {
	batch := delivery.Batch
	if c.updateInboundAnchors(ctx, []IngestDelivery{delivery}) {
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

	if err := c.sweepStale(ctx, batch); err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "stale posting sweep", err)
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

	c.recordFetch(ctx, batch)
	if !c.completeObservations(ctx, []IngestDelivery{delivery}) {
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

	doc := documentFromIngestWithSafety(batch.Document, c.safety)
	docs, err := c.canonicalDocuments(ctx, []documentstore.Document{doc})
	if err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "document canonicalization", err)

		return true
	}
	docs, err = c.clusterDocuments(ctx, docs)
	if err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "content cluster", err)

		return true
	}
	receipt, err := c.documents.Receive(ctx, docs)
	if err != nil {
		c.redeliver(ctx, delivery, batch.SourceURL, "document store", err)
		return true
	}
	if receipt.Busy {
		c.redeliver(ctx, delivery, batch.SourceURL, "document store at capacity", nil)
		return true
	}
	docs = c.committedDocuments(receipt, docs)
	if c.index != nil {
		if err := c.indexDocuments(ctx, docs); err != nil {
			c.redeliver(ctx, delivery, batch.SourceURL, "search index", err)
			return true
		}
	}

	return false
}

// qualityRejectionRule names the quality rule the batch's document text
// violates, or "" when the batch may be absorbed (no gate, no document, or
// text worth indexing). The gate's Gopher/C4 heuristics model WEB PAGES —
// word-count floors and boilerplate ratios; a parsed document format (PDF
// bulletin, vCard, image or audio metadata) legitimately extracts a handful
// of words, so only web-page content types face the gate (CRAWL-17 wrap-up:
// a real 1999 PDF was fetched, parsed, and then silently dropped here).
func (c *IngestConsumer) qualityRejectionRule(batch yagocrawlcontract.IngestBatch) string {
	if c.quality == nil || !hasDocument(batch.Document) ||
		batch.Document.ExtractedText == "" ||
		!webPageContentType(batch.Document.ContentType) {
		return ""
	}

	return c.quality(batch.Document.ExtractedText)
}

// webPageContentType reports whether the document is an ordinary web page —
// the corpus the spam heuristics were built for.
func webPageContentType(contentType string) bool {
	mediaType, _, _ := strings.Cut(contentType, ";")
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "", "text/html", "application/xhtml+xml", "text/plain":
		return true
	default:
		return false
	}
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
	return documentFromIngestWithSafety(doc, nil)
}

func documentFromIngestWithSafety(
	doc yagocrawlcontract.DocumentIngest,
	classifier ContentSafetyClassifier,
) documentstore.Document {
	return documentstore.Document{
		CanonicalURL:                doc.CanonicalURL,
		NormalizedURL:               doc.NormalizedURL,
		Title:                       doc.Title,
		Headings:                    doc.Headings,
		ExtractedText:               doc.ExtractedText,
		ContentQuality:              contentQualityFromText(doc.ExtractedText),
		ContentSafety:               contentSafetyFromIngest(doc, classifier),
		RawContentReference:         doc.RawContentReference,
		Language:                    doc.Language,
		ContentType:                 doc.ContentType,
		FetchStatus:                 doc.FetchStatus,
		FetchedAt:                   doc.FetchedAt,
		IndexedAt:                   doc.IndexedAt,
		PublishedAt:                 doc.PublishedAt,
		ModifiedAt:                  doc.ModifiedAt,
		FirstSeenAt:                 doc.FirstSeenAt,
		ContentChangedAt:            doc.ContentChangedAt,
		DateConfidence:              doc.DateConfidence,
		DateSource:                  doc.DateSource,
		ContentHash:                 doc.ContentHash,
		Outlinks:                    doc.Outlinks,
		Inlinks:                     anchorTextFromIngest(doc.Inlinks),
		OutboundAnchors:             outboundAnchorsFromIngest(doc.OutboundAnchors),
		OutboundAnchorEvidenceKnown: doc.OutboundAnchorEvidenceKnown,
		Images:                      imageMetadataFromIngest(doc.Images),
		Metadata:                    doc.Metadata,
	}
}

func outboundAnchorsFromIngest(
	in []yagocrawlcontract.OutboundAnchor,
) []documentstore.OutboundAnchor {
	out := make([]documentstore.OutboundAnchor, 0, len(in))
	for _, anchor := range in {
		out = append(out, documentstore.OutboundAnchor{
			TargetURL:     anchor.TargetURL,
			Text:          anchor.Text,
			NoFollow:      anchor.NoFollow,
			UserGenerated: anchor.UserGenerated,
			Sponsored:     anchor.Sponsored,
		})
	}

	return out
}

func anchorTextFromIngest(in []yagocrawlcontract.AnchorText) []documentstore.AnchorText {
	out := make([]documentstore.AnchorText, 0, len(in))
	for _, anchor := range in {
		out = append(out, documentstore.AnchorText{
			URL:           anchor.URL,
			Text:          anchor.Text,
			NoFollow:      anchor.NoFollow,
			UserGenerated: anchor.UserGenerated,
			Sponsored:     anchor.Sponsored,
		})
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

// sweepStale purges the source URL's postings for words the fresh batch no
// longer carries (RWI-01), so a changed page stops matching its removed words.
// A batch without postings is left untouched: partial ingests (and the DHT
// intake, which never passes here) must not wipe a URL's index.
func (c *IngestConsumer) sweepStale(
	ctx context.Context,
	batch yagocrawlcontract.IngestBatch,
) error {
	if len(batch.Postings) == 0 {
		return nil
	}
	urlHash, err := c.hashURL(batch.SourceURL)
	if err != nil {
		return fmt.Errorf("hash source url: %w", err)
	}
	live := make(map[yagomodel.Hash]struct{}, len(batch.Postings))
	for _, posting := range batch.Postings {
		live[posting.WordHash] = struct{}{}
	}
	if _, err := c.stale.PurgeStalePostings(ctx, urlHash.Hash(), live); err != nil {
		return fmt.Errorf("purge stale postings: %w", err)
	}

	return nil
}
