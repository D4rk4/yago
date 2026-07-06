package crawlresults

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

// ingestMicroBatch caps how many pending deliveries one loop turn groups:
// grouped documents land in the vault through one Receive and in bleve through
// one batch per shard, amortizing the per-write flush that made
// document-at-a-time ingest the crawl bottleneck (PERF-05).
const ingestMicroBatch = 16

// drainPending returns the received delivery plus whatever is already waiting
// on the stream, without blocking, up to the micro-batch cap.
func (c *IngestConsumer) drainPending(first IngestDelivery) []IngestDelivery {
	group := []IngestDelivery{first}
	for len(group) < ingestMicroBatch {
		select {
		case delivery, ok := <-c.stream.Receive():
			if !ok {
				return group
			}
			group = append(group, delivery)
		default:
			return group
		}
	}

	return group
}

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

	withDocs := make([]IngestDelivery, 0, len(admitted))
	docs := make([]documentstore.Document, 0, len(admitted))
	tail := make([]IngestDelivery, 0, len(admitted))
	for _, delivery := range admitted {
		batch := delivery.Batch
		if c.documents == nil || !hasDocument(batch.Document) {
			tail = append(tail, delivery)

			continue
		}
		doc := documentFromIngest(batch.Document)
		if c.collapseNearDuplicate(ctx, doc) {
			tail = append(tail, delivery)

			continue
		}
		withDocs = append(withDocs, delivery)
		docs = append(docs, doc)
	}

	if len(docs) > 0 && !c.storeDocumentGroup(ctx, withDocs, docs) {
		withDocs = nil
	}
	for _, delivery := range append(tail, withDocs...) {
		c.absorbTail(ctx, delivery)
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
	receipt, err := c.documents.Receive(ctx, docs)
	if err != nil {
		c.redeliverGroup(ctx, deliveries, "document store", err)

		return false
	}
	if receipt.Busy {
		c.redeliverGroup(ctx, deliveries, "document store at capacity", nil)

		return false
	}
	if c.index == nil {
		return true
	}
	if err := c.indexDocuments(ctx, docs); err != nil {
		c.redeliverGroup(ctx, deliveries, "search index", err)

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
