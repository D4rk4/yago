package searchindex

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// BatchIndexer is the optional bulk path a SearchIndex may offer: indexing N
// documents through one bleve batch per shard amortizes the per-write segment
// flush that made document-at-a-time ingest the crawl bottleneck (PERF-05).
// Callers type-assert and fall back to per-document Index when absent.
type BatchIndexer interface {
	IndexBatch(ctx context.Context, docs []documentstore.Document) error
}

var stageBatchDocument = func(batch *bleve.Batch, id string, doc bleveDocument) error {
	return batch.Index(id, doc)
}

// IndexBatch indexes the documents through one bleve batch per shard. An empty
// slice is a no-op; a document without an id fails the whole batch, matching
// the single-document contract.
func (b *BleveDiskIndex) IndexBatch(
	ctx context.Context,
	docs []documentstore.Document,
) error {
	if len(docs) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return fmt.Errorf("search index closed")
	}
	b.mutationMu.Lock()
	defer b.mutationMu.Unlock()
	batches := make([]*bleve.Batch, len(b.shards))
	for _, doc := range docs {
		id := documentID(doc)
		if id == "" {
			return fmt.Errorf("document id required")
		}
		shardNumber := diskShardNumber(b.shards, id)
		shard := b.shards[shardNumber]
		batch := batches[shardNumber]
		if batch == nil {
			batch = shard.NewBatch()
			batches[shardNumber] = batch
		}
		indexed, err := bleveDocumentFromStore(doc)
		if err != nil {
			return err
		}
		if err := stageBatchDocument(batch, id, indexed); err != nil {
			return fmt.Errorf("stage document: %w", err)
		}
	}
	if err := commitBleveShardBatches(b.shards, batches); err != nil {
		return err
	}
	b.markUpdated()

	return nil
}

// IndexBatch delegates to the inner index's bulk path when it offers one and
// falls back to per-document indexing otherwise; either way the cache resets
// once per batch instead of once per document.
func (c *CachedSearchIndex) IndexBatch(
	ctx context.Context,
	docs []documentstore.Document,
) error {
	if bulk, ok := c.inner.(BatchIndexer); ok {
		if err := bulk.IndexBatch(ctx, docs); err != nil {
			return fmt.Errorf("cached index batch: %w", err)
		}
		c.invalidate()

		return nil
	}
	for _, doc := range docs {
		if err := c.inner.Index(ctx, doc); err != nil {
			return fmt.Errorf("cached index batch: %w", err)
		}
	}
	c.invalidate()

	return nil
}
