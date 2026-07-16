package crawlresults

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

// ingestMicroBatch caps how many pending deliveries one loop turn groups:
// grouped documents land in the vault through one Receive and in bleve through
// one batch per shard, amortizing the per-write flush that made
// document-at-a-time ingest the crawl bottleneck (PERF-05).
const ingestMicroBatch = 16

// absorbGroup absorbs the deliveries as one unit: admission gates run per
// delivery, the surviving documents are stored and indexed together, and the
// per-delivery tail (metadata, postings, ack) runs for everything that made it
// through. A single delivery takes the plain absorb path unchanged.
func (c *IngestConsumer) absorbGroup(ctx context.Context, group []IngestDelivery) {
	if len(group) == 1 {
		c.absorb(ctx, group[0])

		return
	}

	admitted := make([]IngestDelivery, 0, len(group))
	for _, delivery := range group {
		if c.passesGates(ctx, delivery) {
			admitted = append(admitted, delivery)
		}
	}
	admitted = coalesceIngestDeliveries(admitted)
	admitted = c.beginObservations(ctx, admitted)

	regular := make([]IngestDelivery, 0, len(admitted))
	for _, delivery := range admitted {
		if delivery.Batch.Removed {
			c.purgeRemoval(ctx, delivery)
			continue
		}
		regular = append(regular, delivery)
	}
	reservation, err := c.reserveIngestDocumentLineages(ctx, regular)
	if err != nil {
		c.redeliverGroup(ctx, regular, "document lineage reservation", err)

		return
	}
	defer c.releaseIngestDocumentLineages(reservation)

	withDocs := make([]IngestDelivery, 0, len(regular))
	docs := make([]documentstore.Document, 0, len(regular))
	tail := make([]IngestDelivery, 0, len(regular))
	for _, delivery := range regular {
		batch := delivery.Batch
		if c.documents == nil || !hasDocument(batch.Document) {
			tail = append(tail, delivery)

			continue
		}
		doc := documentFromIngestWithSafety(batch.Document, c.safety)
		withDocs = append(withDocs, delivery)
		docs = append(docs, doc)
	}

	if len(docs) > 0 && !c.storeReservedDocumentGroup(
		ctx,
		withDocs,
		docs,
		reservation,
	) {
		withDocs = nil
	}
	c.absorbReservedTailGroup(ctx, append(tail, withDocs...), reservation)
}

// absorbTailGroup persists the group's URL metadata, stale sweeps, postings,
// and recrawl schedule through one store call each — one durable commit per
// touched shard instead of one per page (IO-AGG-01) — then acknowledges every
// surviving delivery. Group-level failures redeliver the whole group, the same
// coarsening the document group already accepts.
func (c *IngestConsumer) absorbTailGroup(ctx context.Context, group []IngestDelivery) {
	c.absorbReservedTailGroup(ctx, group, nil)
}

func (c *IngestConsumer) absorbReservedTailGroup(
	ctx context.Context,
	group []IngestDelivery,
	reservation documentstore.DocumentLineageReservation,
) {
	if len(group) == 0 {
		return
	}
	if len(group) == 1 {
		c.absorbReservedTail(ctx, group[0], reservation)

		return
	}
	if c.updateReservedInboundAnchors(ctx, group, reservation) {
		return
	}
	metadata := make([]yagomodel.URIMetadataRow, 0, len(group))
	for _, delivery := range group {
		metadata = append(metadata, delivery.Batch.Metadata...)
	}
	urlReceipt, err := c.urls.Receive(ctx, metadata)
	if err != nil {
		c.redeliverGroup(ctx, group, "url metadata store", err)

		return
	}
	if urlReceipt.Busy {
		c.redeliverGroup(ctx, group, "url metadata at capacity", nil)

		return
	}

	group = c.sweepStaleGroup(ctx, group)
	if len(group) == 0 {
		return
	}

	postings := make([]yagomodel.RWIPosting, 0, len(group))
	for _, delivery := range group {
		postings = append(postings, delivery.Batch.Postings...)
	}
	postingReceipt, err := c.postings.Receive(ctx, postings)
	if err != nil {
		c.redeliverGroup(ctx, group, "posting store", err)

		return
	}
	if postingReceipt.Busy {
		c.redeliverGroup(ctx, group, "posting store at capacity", nil)

		return
	}

	c.recordFetchGroup(ctx, group)
	if !c.completeObservations(ctx, group) {
		return
	}
	for _, delivery := range group {
		c.finishAbsorbed(ctx, delivery)
	}
}

// finishAbsorbed counts, acknowledges, and logs one absorbed delivery after
// its group's stores committed.
func (c *IngestConsumer) finishAbsorbed(ctx context.Context, delivery IngestDelivery) {
	batch := delivery.Batch
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

// staleBatchSweeper is the optional sweeper capability that purges a whole
// micro-batch's stale postings in one transaction; a sweeper without it falls
// back to the per-page sweep.
type staleBatchSweeper interface {
	PurgeStalePostingsForURLs(
		ctx context.Context,
		staleByURL map[yagomodel.Hash]map[yagomodel.Hash]struct{},
	) (int, error)
}

// sweepStaleGroup runs the RWI-01 stale sweep for the group and returns the
// deliveries that may proceed; failures redeliver (the whole group under the
// batch capability, per delivery on the fallback path).
func (c *IngestConsumer) sweepStaleGroup(
	ctx context.Context,
	group []IngestDelivery,
) []IngestDelivery {
	batchSweeper, batched := c.stale.(staleBatchSweeper)
	if !batched {
		kept := group[:0]
		for _, delivery := range group {
			if err := c.sweepStale(ctx, delivery.Batch); err != nil {
				c.redeliver(ctx, delivery, delivery.Batch.SourceURL, "stale posting sweep", err)

				continue
			}
			kept = append(kept, delivery)
		}

		return kept
	}

	staleByURL := make(map[yagomodel.Hash]map[yagomodel.Hash]struct{}, len(group))
	kept := group[:0]
	for _, delivery := range group {
		batch := delivery.Batch
		if len(batch.Postings) == 0 {
			kept = append(kept, delivery)

			continue
		}
		urlHash, err := c.hashURL(batch.SourceURL)
		if err != nil {
			c.redeliver(ctx, delivery, batch.SourceURL, "stale posting sweep", err)

			continue
		}
		live := make(map[yagomodel.Hash]struct{}, len(batch.Postings))
		for _, posting := range batch.Postings {
			live[posting.WordHash] = struct{}{}
		}
		staleByURL[urlHash.Hash()] = live
		kept = append(kept, delivery)
	}
	if len(staleByURL) == 0 {
		return kept
	}
	if _, err := batchSweeper.PurgeStalePostingsForURLs(ctx, staleByURL); err != nil {
		c.redeliverGroup(ctx, kept, "stale posting sweep", err)

		return nil
	}

	return kept
}

// fetchBatchRecorder is the optional recorder capability that schedules a whole
// micro-batch's recrawls in one transaction; a recorder without it falls back
// to the per-page best-effort record.
type fetchBatchRecorder interface {
	RecordFetches(ctx context.Context, urls, profileHandles []string, fetchedAt []time.Time) error
}

// recordFetchGroup feeds the recrawl schedule for every real page fetch in the
// group. Like recordFetch it is best-effort: a failure is logged, never
// propagated, so it cannot fail an ingest that already stored its data.
func (c *IngestConsumer) recordFetchGroup(ctx context.Context, group []IngestDelivery) {
	recorder, batched := c.recorder.(fetchBatchRecorder)
	if !batched {
		for _, delivery := range group {
			c.recordFetch(ctx, delivery.Batch)
		}

		return
	}
	urls := make([]string, 0, len(group))
	handles := make([]string, 0, len(group))
	fetched := make([]time.Time, 0, len(group))
	for _, delivery := range group {
		batch := delivery.Batch
		if batch.SourceURL == "" ||
			!hasDocument(batch.Document) ||
			batch.Document.FetchedAt.IsZero() {
			continue
		}
		urls = append(urls, batch.SourceURL)
		handles = append(handles, batch.ProfileHandle)
		fetched = append(fetched, batch.Document.FetchedAt)
	}
	if len(urls) == 0 {
		return
	}
	if err := recorder.RecordFetches(ctx, urls, handles, fetched); err != nil {
		slog.WarnContext(ctx, msgRecrawlRecordFailed, slog.Any("error", err))
	}
}

// storeDocumentGroup persists the documents through one vault Receive and one
// index batch; on failure or backpressure every carrying delivery is
// redelivered so the store and the index stay in step, and the group reports
// false.
func (c *IngestConsumer) storeDocumentGroup(
	ctx context.Context,
	deliveries []IngestDelivery,
	docs []documentstore.Document,
) bool {
	return c.storeReservedDocumentGroup(ctx, deliveries, docs, nil)
}

func (c *IngestConsumer) storeReservedDocumentGroup(
	ctx context.Context,
	deliveries []IngestDelivery,
	docs []documentstore.Document,
	reservation documentstore.DocumentLineageReservation,
) bool {
	canonical, err := c.canonicalIngestDocuments(ctx, reservation, docs)
	if err != nil {
		c.redeliverGroup(ctx, deliveries, "document canonicalization", err)

		return false
	}
	projection, err := c.prepareDocumentClusters(ctx, canonical)
	if err != nil {
		c.redeliverGroup(ctx, deliveries, "content cluster", err)

		return false
	}
	defer projection.release()
	docs = projection.documents
	receipt, err := c.documents.Receive(ctx, docs)
	if err != nil {
		c.redeliverGroup(ctx, deliveries, "document store", err)

		return false
	}
	if receipt.Busy {
		c.redeliverGroup(ctx, deliveries, "document store at capacity", nil)

		return false
	}
	docs = c.committedDocuments(receipt, docs)
	if projection.replay {
		docs = projection.documents
	}
	if c.index != nil {
		if err := c.indexDocuments(ctx, docs); err != nil {
			c.redeliverGroup(ctx, deliveries, "search index", err)

			return false
		}
	}
	if err := projection.finalize(ctx); err != nil {
		c.redeliverGroup(ctx, deliveries, "content cluster finalization", err)

		return false
	}

	return true
}

// indexDocuments prefers the index's bulk path and falls back to per-document
// indexing when the backend offers none.
func (c *IngestConsumer) indexDocuments(
	ctx context.Context,
	docs []documentstore.Document,
) error {
	started := time.Now()
	err := c.writeSearchIndexDocuments(ctx, docs)
	if c.indexWrites != nil {
		c.indexWrites.ObserveSearchIndexWrite(time.Since(started), len(docs), err != nil)
	}

	return err
}

func (c *IngestConsumer) writeSearchIndexDocuments(
	ctx context.Context,
	docs []documentstore.Document,
) error {
	if bulk, ok := c.index.(searchindex.BatchIndexer); ok {
		//nolint:wrapcheck // the caller labels the failure for redelivery.
		return bulk.IndexBatch(ctx, docs)
	}
	for _, doc := range docs {
		if err := c.index.Index(ctx, doc); err != nil {
			//nolint:wrapcheck // the caller labels the failure for redelivery.
			return err
		}
	}

	return nil
}

func (c *IngestConsumer) redeliverGroup(
	ctx context.Context,
	deliveries []IngestDelivery,
	reason string,
	cause error,
) {
	for _, delivery := range deliveries {
		c.redeliver(ctx, delivery, delivery.Batch.SourceURL, reason, cause)
	}
}
