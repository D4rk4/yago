package crawlresults

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

// ingestMicroBatch caps how many pending deliveries one loop turn groups:
// grouped documents land in the vault through one Receive and in bleve through
// one batch per shard, amortizing the per-write flush that made
// document-at-a-time ingest the crawl bottleneck (PERF-05).
const ingestMicroBatch = yagocrawlcontract.MaximumIngestBatchRecords

// absorbGroup absorbs the deliveries as one unit: admission gates run per
// delivery, the surviving documents are stored and indexed together, and the
// per-delivery tail (metadata, postings, ack) runs for everything that made it
// through. A single delivery takes the plain absorb path unchanged.
func (c *IngestConsumer) absorbGroup(ctx context.Context, group []IngestDelivery) {
	if len(group) > 1 {
		segments := splitIngestGroupByImportMode(group)
		if len(segments) > 1 {
			for _, segment := range segments {
				c.absorbGroup(ctx, segment)
			}
			return
		}
	}
	if len(group) == 1 {
		c.absorb(ctx, group[0])

		return
	}
	finishAuthorization := c.measureIngestStage(IngestStageDeliveryAuthorization)
	group, releases := authorizeIngestGroup(ctx, group)
	finishAuthorization()
	defer releaseIngestGroup(releases)
	group = c.absorbNotModifiedDeliveries(ctx, group)

	finishAdmission := c.measureIngestStage(IngestStageDeliveryAdmission)
	admitted := make([]IngestDelivery, 0, len(group))
	for _, delivery := range group {
		if c.passesGates(ctx, delivery) {
			admitted = append(admitted, delivery)
		}
	}
	admitted = coalesceIngestDeliveries(admitted)
	finishAdmission()
	regular, removals := splitIngestRemovals(admitted)
	finishObservation := c.measureIngestStage(IngestStageObservationHistory)
	removals = c.beginObservations(ctx, removals)
	finishObservation()
	for _, delivery := range removals {
		c.purgeRemoval(ctx, delivery)
	}
	finishAdmission = c.measureIngestStage(IngestStageStorageAdmission)
	storageAdmitted := c.admitStorageGrowth(ctx, regular)
	finishAdmission()
	if !storageAdmitted {
		return
	}
	finishObservation = c.measureIngestStage(IngestStageObservationHistory)
	regular = c.beginObservations(ctx, regular)
	finishObservation()
	finishReservation := c.measureIngestStage(IngestStageLineageReservation)
	reservation, err := c.reserveIngestDocumentLineages(ctx, regular)
	finishReservation()
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

func splitIngestRemovals(group []IngestDelivery) ([]IngestDelivery, []IngestDelivery) {
	regular := make([]IngestDelivery, 0, len(group))
	removals := make([]IngestDelivery, 0, len(group))
	for _, delivery := range group {
		if delivery.Batch.Removed {
			removals = append(removals, delivery)
		} else {
			regular = append(regular, delivery)
		}
	}
	return regular, removals
}

func splitIngestGroupByImportMode(group []IngestDelivery) [][]IngestDelivery {
	segments := make([][]IngestDelivery, 0, 2)
	start := 0
	for index := 1; index <= len(group); index++ {
		if index < len(group) && group[index].ImportOnly == group[start].ImportOnly {
			continue
		}
		segments = append(segments, group[start:index])
		start = index
	}

	return segments
}

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
	finishAnchors := c.measureIngestStage(IngestStageOutboundAnchorUpdate)
	anchorsDeferred := c.updateReservedInboundAnchors(ctx, group, reservation)
	finishAnchors()
	if anchorsDeferred {
		return
	}
	metadata := make([]yagomodel.URIMetadataRow, 0, len(group))
	for _, delivery := range group {
		metadata = append(metadata, delivery.Batch.Metadata...)
	}
	started := time.Now()
	urlReceipt, err := c.urls.Receive(ctx, metadata)
	c.observeIngestStage(IngestStageURLMetadataWrite, time.Since(started))
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
	started = time.Now()
	postingReceipt, err := receivePostingChunks(ctx, c.postings, postings)
	c.observeIngestStage(IngestStageRWIPostingWrite, time.Since(started))
	if err != nil {
		c.redeliverGroup(ctx, group, "posting store", err)

		return
	}
	if postingReceipt.Busy {
		c.redeliverGroup(ctx, group, "posting store at capacity", nil)

		return
	}

	finishRecrawl := c.measureIngestStage(IngestStageRecrawlSchedule)
	c.recordFetchGroup(ctx, group)
	finishRecrawl()
	finishObservation := c.measureIngestStage(IngestStageObservationHistory)
	observationsComplete := c.completeObservations(ctx, group)
	finishObservation()
	if !observationsComplete {
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
	if c.observer != nil {
		c.observer.ObserveAbsorbed(
			len(batch.Document.ExtractedText),
			len(batch.Metadata),
			len(batch.Postings),
		)
	}
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
	started := time.Now()
	defer func() {
		c.observeIngestStage(IngestStageStalePostingSweep, time.Since(started))
	}()
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
	hintedRecorder, hinted := c.recorder.(sourceModifiedFetchBatchRecorder)
	batchRecorder, batched := c.recorder.(fetchBatchRecorder)
	profileRecorder, profileBatched := c.recorder.(profileFetchBatchRecorder)
	if !hinted && !batched && !profileBatched {
		for _, delivery := range group {
			c.recordFetch(ctx, delivery)
		}

		return
	}
	urls := make([]string, 0, len(group))
	handles := make([]string, 0, len(group))
	fetched := make([]time.Time, 0, len(group))
	modified := make([]time.Time, 0, len(group))
	profiles := make([]yagocrawlcontract.CrawlProfile, 0, len(group))
	profilesComplete := true
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
		modified = append(modified, batch.SourceModifiedAt)
		if delivery.CrawlProfile == nil ||
			delivery.CrawlProfile.Handle != batch.ProfileHandle {
			profilesComplete = false
		} else {
			profiles = append(profiles, *delivery.CrawlProfile)
		}
	}
	if len(urls) == 0 {
		return
	}
	var err error
	switch {
	case profileBatched && profilesComplete:
		err = profileRecorder.RecordProfileFetches(ctx, urls, profiles, fetched, modified)
	case hinted:
		err = hintedRecorder.RecordFetchesWithSourceModified(
			ctx,
			urls,
			handles,
			fetched,
			modified,
		)
	case batched:
		err = batchRecorder.RecordFetches(ctx, urls, handles, fetched)
	default:
		for _, delivery := range group {
			c.recordFetch(ctx, delivery)
		}

		return
	}
	if err != nil {
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
	finishCanonicalization := c.measureIngestStage(IngestStageDocumentCanonicalization)
	canonical, err := c.canonicalIngestDocuments(ctx, reservation, docs)
	finishCanonicalization()
	if err != nil {
		c.redeliverGroup(ctx, deliveries, "document canonicalization", err)

		return false
	}
	finishClusterPreparation := c.measureIngestStage(IngestStageContentClusterPreparation)
	projection, err := c.prepareIngestDocumentClusters(ctx, deliveries, canonical)
	finishClusterPreparation()
	if err != nil {
		c.redeliverGroup(ctx, deliveries, "content cluster", err)

		return false
	}
	defer projection.release()
	docs = projection.documents
	started := time.Now()
	receipt, err := c.documents.Receive(ctx, docs)
	c.observeIngestStage(IngestStageDocumentWrite, time.Since(started))
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
	finishClusterFinalization := c.measureIngestStage(IngestStageContentClusterFinalization)
	finalizationErr := projection.finalize(ctx)
	finishClusterFinalization()
	if finalizationErr != nil {
		c.redeliverGroup(ctx, deliveries, "content cluster finalization", finalizationErr)

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
	c.observeIngestStage(IngestStageSearchIndexWrite, time.Since(started))
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
