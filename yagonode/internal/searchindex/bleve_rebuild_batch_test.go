package searchindex

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestBleveRebuildUsesBoundedBatches(t *testing.T) {
	documents := make([]documentstore.Document, bleveRebuildBatchDocuments*2+3)
	for position := range documents {
		documents[position].NormalizedURL = "https://example.org/" + string(rune('a'+position))
	}
	stored := &fakeStoredDocuments{documents: documents}
	previous := indexBleveRebuildBatch
	var sizes []int
	indexBleveRebuildBatch = func(
		_ *BleveDiskIndex,
		_ context.Context,
		batch []documentstore.Document,
	) error {
		sizes = append(sizes, len(batch))

		return nil
	}
	t.Cleanup(func() { indexBleveRebuildBatch = previous })

	if err := (&BleveDiskIndex{}).rebuild(t.Context(), stored); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	want := []int{bleveRebuildBatchDocuments, bleveRebuildBatchDocuments, 3}
	if !reflect.DeepEqual(sizes, want) || stored.scans != 1 {
		t.Fatalf("rebuild batches = %v scans=%d", sizes, stored.scans)
	}
}

func TestBleveRebuildPropagatesBatchAndScanFailures(t *testing.T) {
	sentinel := errors.New("batch failed")
	previous := indexBleveRebuildBatch
	indexBleveRebuildBatch = func(
		_ *BleveDiskIndex,
		_ context.Context,
		_ []documentstore.Document,
	) error {
		return sentinel
	}
	t.Cleanup(func() { indexBleveRebuildBatch = previous })

	documents := make([]documentstore.Document, bleveRebuildBatchDocuments)
	if err := (&BleveDiskIndex{}).rebuild(
		t.Context(),
		&fakeStoredDocuments{documents: documents},
	); !errors.Is(err, sentinel) {
		t.Fatalf("batch failure = %v", err)
	}
	if err := (&BleveDiskIndex{}).rebuild(
		t.Context(),
		&fakeStoredDocuments{err: sentinel},
	); !errors.Is(err, sentinel) {
		t.Fatalf("scan failure = %v", err)
	}
}

func TestBleveRebuildSkipsEmptyBatch(t *testing.T) {
	previous := indexBleveRebuildBatch
	indexBleveRebuildBatch = func(
		_ *BleveDiskIndex,
		_ context.Context,
		_ []documentstore.Document,
	) error {
		t.Fatal("empty rebuild indexed a batch")

		return nil
	}
	t.Cleanup(func() { indexBleveRebuildBatch = previous })

	if err := (&BleveDiskIndex{}).rebuild(
		t.Context(),
		&fakeStoredDocuments{},
	); err != nil {
		t.Fatalf("empty rebuild: %v", err)
	}
}
