package searchindex

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type stagedBleveRebuildAdmission struct {
	allowed int
	calls   int
	err     error
}

func TestBleveRebuildRestartsAfterStoragePressure(t *testing.T) {
	path := writeLegacySearchIndex(t) + string(os.PathSeparator)
	documents := make([]documentstore.Document, bleveRebuildBatchDocuments+1)
	for index := range documents {
		documents[index] = documentstore.Document{
			NormalizedURL: "https://example.org/restart/" + string(rune('a'+index)),
			Title:         "restartable pressure document",
			Language:      "en",
		}
	}
	directory := newFakeDocumentDirectory(documents...)
	sentinel := errors.New("pressure")
	admission := &stagedBleveRebuildAdmission{allowed: 1, err: sentinel}
	index, err := NewBleveDiskIndex(
		t.Context(),
		path,
		directory,
		&fakeStoredDocuments{documents: documents},
		admission,
	)
	if !errors.Is(err, sentinel) || index != nil {
		t.Fatalf("pressured rebuild index=%v error=%v", index, err)
	}
	if _, err := os.Stat(bleveRebuildStatePath(path)); err != nil {
		t.Fatalf("pressured rebuild state: %v", err)
	}

	restarted := &fakeStoredDocuments{documents: documents}
	index, err = NewBleveDiskIndex(t.Context(), path, directory, restarted)
	if err != nil {
		t.Fatalf("restart rebuild: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })
	if restarted.scans != 1 {
		t.Fatalf("restart scans = %d, want 1", restarted.scans)
	}
	if _, err := os.Stat(bleveRebuildStatePath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed rebuild state: %v", err)
	}
	results, err := index.Search(t.Context(), SearchRequest{
		Query: "restartable", Terms: []string{"restartable"}, MaxResults: len(documents),
	})
	if err != nil || len(results.Results) != len(documents) {
		t.Fatalf("restart results = %d, %v", len(results.Results), err)
	}
}

func (admission *stagedBleveRebuildAdmission) CheckGrowth() error {
	admission.calls++
	if admission.calls > admission.allowed {
		return admission.err
	}

	return nil
}

func TestBleveRebuildChecksStorageBeforeEveryBatch(t *testing.T) {
	original := indexBleveRebuildBatch
	t.Cleanup(func() { indexBleveRebuildBatch = original })
	batches := 0
	indexed := 0
	indexBleveRebuildBatch = func(
		_ *BleveDiskIndex,
		_ context.Context,
		documents []documentstore.Document,
	) error {
		batches++
		indexed += len(documents)

		return nil
	}
	documents := make([]documentstore.Document, bleveRebuildBatchDocuments+1)
	stored := &fakeStoredDocuments{documents: documents}
	sentinel := errors.New("pressure")
	admission := &stagedBleveRebuildAdmission{allowed: 1, err: sentinel}
	err := (&BleveDiskIndex{}).rebuild(t.Context(), stored, admission)
	if !errors.Is(err, sentinel) || batches != 1 || indexed != bleveRebuildBatchDocuments ||
		admission.calls != 2 {
		t.Fatalf(
			"rebuild error=%v batches=%d indexed=%d admission=%d",
			err,
			batches,
			indexed,
			admission.calls,
		)
	}
}

func TestBleveRebuildGrowthAdmissionSelection(t *testing.T) {
	if firstBleveRebuildGrowthAdmission(nil) != nil {
		t.Fatal("empty admission selection returned a value")
	}
	admission := &stagedBleveRebuildAdmission{}
	if firstBleveRebuildGrowthAdmission([]BleveRebuildGrowthAdmission{admission}) != admission {
		t.Fatal("configured rebuild admission not selected")
	}
}
