package searchindex

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func batchDoc(url, title string) documentstore.Document {
	return documentstore.Document{
		NormalizedURL: url,
		CanonicalURL:  url,
		Title:         title,
		ExtractedText: "batch indexed body about " + title,
	}
}

func TestBleveDiskIndexBatchIndexesAcrossShards(t *testing.T) {
	docs := []documentstore.Document{
		batchDoc("https://a.example/one", "alpha"),
		batchDoc("https://b.example/two", "beta"),
		batchDoc("https://c.example/three", "gamma"),
	}
	index, err := NewBleveDiskIndex(
		t.Context(),
		t.TempDir(),
		newFakeDocumentDirectory(docs...),
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	defer func() { _ = index.Close() }()
	if err := index.IndexBatch(t.Context(), docs); err != nil {
		t.Fatalf("IndexBatch: %v", err)
	}

	stats, err := index.Stats(t.Context())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Documents != len(docs) {
		t.Fatalf("documents = %d, want %d", stats.Documents, len(docs))
	}
	hits, err := index.Search(t.Context(), SearchRequest{Query: "beta", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits.Results) != 1 {
		t.Fatalf("batch-indexed document not searchable: %+v", hits)
	}

	if err := index.IndexBatch(t.Context(), nil); err != nil {
		t.Fatalf("empty batch must be a no-op: %v", err)
	}
	if err := index.IndexBatch(
		t.Context(),
		[]documentstore.Document{{Title: "no id"}},
	); err == nil {
		t.Fatal("a document without an id must fail the batch")
	}
}

func TestBleveDiskIndexBatchContextAndClosed(t *testing.T) {
	docs := []documentstore.Document{batchDoc("https://a.example/one", "alpha")}
	index, err := NewBleveDiskIndex(
		t.Context(),
		t.TempDir(),
		newFakeDocumentDirectory(docs...),
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	defer func() { _ = index.Close() }()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := index.IndexBatch(ctx, docs); err == nil {
		t.Fatal("a cancelled context must fail the batch")
	}

	if err := index.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := index.IndexBatch(t.Context(), docs); err == nil {
		t.Fatal("a closed index must fail the batch")
	}
}

func TestBleveDiskIndexBatchPropagatesShardWriteError(t *testing.T) {
	docs := []documentstore.Document{batchDoc("https://a.example/one", "alpha")}
	index, err := NewBleveDiskIndex(
		t.Context(),
		t.TempDir(),
		newFakeDocumentDirectory(docs...),
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	// Closing the underlying shards while the index still reads open forces the
	// per-shard bleve batch write to fail, exercising the shard.Batch error path.
	closeBleveShards(index.shards)
	if err := index.IndexBatch(t.Context(), docs); err == nil {
		t.Fatal("a closed underlying shard must fail the batch write")
	}
}

type bulkRecordingInner struct {
	SearchIndex

	batches int
	fail    bool
}

func (b *bulkRecordingInner) IndexBatch(context.Context, []documentstore.Document) error {
	b.batches++
	if b.fail {
		return errors.New("bulk failed")
	}

	return nil
}

type perDocInner struct {
	SearchIndex

	indexed int
	fail    bool
}

func (p *perDocInner) Index(context.Context, documentstore.Document) error {
	p.indexed++
	if p.fail {
		return errors.New("index failed")
	}

	return nil
}

func TestCachedIndexBatchDelegatesAndFallsBack(t *testing.T) {
	bulk := &bulkRecordingInner{}
	cached := NewCachedSearchIndex(bulk, 8)
	docs := []documentstore.Document{batchDoc("https://a.example/", "alpha")}
	if err := cached.IndexBatch(context.Background(), docs); err != nil {
		t.Fatalf("IndexBatch: %v", err)
	}
	if bulk.batches != 1 {
		t.Fatalf("bulk path used %d times, want 1", bulk.batches)
	}
	bulk.fail = true
	if err := cached.IndexBatch(context.Background(), docs); err == nil {
		t.Fatal("bulk failure must propagate")
	}

	perDoc := &perDocInner{}
	cached = NewCachedSearchIndex(perDoc, 8)
	if err := cached.IndexBatch(context.Background(), append(docs, docs...)); err != nil {
		t.Fatalf("fallback IndexBatch: %v", err)
	}
	if perDoc.indexed != 2 {
		t.Fatalf("fallback indexed %d docs, want 2", perDoc.indexed)
	}
	perDoc.fail = true
	if err := cached.IndexBatch(context.Background(), docs); err == nil {
		t.Fatal("per-document failure must propagate")
	}
}

// TestSearchStopsHydratingAtFullPageWithoutFilters pins PERF-03: a filter-less
// facet-less query loads exactly one page of documents from the store, and the
// total comes from bleve rather than a full hit scan.
func TestSearchStopsHydratingAtFullPageWithoutFilters(t *testing.T) {
	docs := make([]documentstore.Document, 0, 12)
	for i := range 12 {
		docs = append(docs, batchDoc(
			fmt.Sprintf("https://host%02d.example/page", i),
			"shared topic",
		))
	}
	directory := newFakeDocumentDirectory(docs...)
	index, err := NewBleveDiskIndex(t.Context(), t.TempDir(), directory, nil)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	defer func() { _ = index.Close() }()
	if err := index.IndexBatch(t.Context(), docs); err != nil {
		t.Fatalf("IndexBatch: %v", err)
	}

	directory.loads = 0
	set, err := index.Search(t.Context(), SearchRequest{Query: "shared", MaxResults: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(set.Results) != 3 || set.Total != 12 {
		t.Fatalf("results=%d total=%d, want 3 rows over an honest bleve total of 12",
			len(set.Results), set.Total)
	}
	if directory.loads != 3 {
		t.Fatalf("hydrated %d documents, want exactly the page (PERF-03)", directory.loads)
	}

	directory.loads = 0
	filtered, err := index.Search(t.Context(), SearchRequest{
		Query: "shared", MaxResults: 3, Language: "ru",
	})
	if err != nil {
		t.Fatalf("filtered Search: %v", err)
	}
	if directory.loads <= 3 {
		t.Fatalf("filtered query hydrated %d documents, must scan past the page", directory.loads)
	}
	if filtered.Total != 0 {
		t.Fatalf("filtered total = %d, want 0 (no Russian docs)", filtered.Total)
	}
}

func TestBleveDiskIndexBatchStageDocumentError(t *testing.T) {
	saved := stageBatchDocument
	t.Cleanup(func() { stageBatchDocument = saved })
	sentinel := errors.New("stage boom")
	stageBatchDocument = func(*bleve.Batch, string, bleveDocument) error { return sentinel }

	docs := []documentstore.Document{batchDoc("https://a.example/one", "alpha")}
	index, err := NewBleveDiskIndex(
		t.Context(),
		t.TempDir(),
		newFakeDocumentDirectory(docs...),
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	if err := index.IndexBatch(t.Context(), docs); !errors.Is(err, sentinel) {
		t.Fatalf("IndexBatch err = %v, want %v", err, sentinel)
	}
}

func TestBleveDiskIndexSerializesMutations(t *testing.T) {
	docs := []documentstore.Document{
		batchDoc("https://a.example/one", "alpha"),
		batchDoc("https://b.example/two", "beta"),
	}
	index, err := NewBleveDiskIndex(
		t.Context(),
		t.TempDir(),
		newFakeDocumentDirectory(docs...),
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })

	saved := stageBatchDocument
	t.Cleanup(func() { stageBatchDocument = saved })
	staging := make(chan struct{})
	release := make(chan struct{})
	stageBatchDocument = func(batch *bleve.Batch, id string, doc bleveDocument) error {
		close(staging)
		<-release

		return saved(batch, id, doc)
	}

	batchDone := make(chan error, 1)
	go func() { batchDone <- index.IndexBatch(t.Context(), docs[:1]) }()
	<-staging
	indexDone := make(chan error, 1)
	go func() { indexDone <- index.Index(t.Context(), docs[1]) }()
	select {
	case err := <-indexDone:
		close(release)
		<-batchDone
		t.Fatalf("concurrent mutation completed before batch staging: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-batchDone; err != nil {
		t.Fatalf("IndexBatch: %v", err)
	}
	if err := <-indexDone; err != nil {
		t.Fatalf("Index: %v", err)
	}
}
