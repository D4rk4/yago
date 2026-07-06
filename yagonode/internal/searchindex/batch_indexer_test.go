package searchindex

import (
	"context"
	"errors"
	"testing"

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
