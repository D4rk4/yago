package searchindex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

var errRebuildInterrupted = errors.New("rebuild interrupted")

type interruptedRebuildDocuments struct {
	document documentstore.Document
}

func (s interruptedRebuildDocuments) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	_, _ = visit(s.document)

	return errRebuildInterrupted
}

func TestBleveDiskIndexRestartsInterruptedMappingRebuild(t *testing.T) {
	path := writeLegacySearchIndex(t) + string(os.PathSeparator)
	documents := []documentstore.Document{
		{NormalizedURL: "https://example.org/first", Title: "first marker", Language: "en"},
		{NormalizedURL: "https://example.org/second", Title: "second marker", Language: "en"},
	}
	directory := newFakeDocumentDirectory(documents...)
	index, err := NewBleveDiskIndex(
		t.Context(),
		path,
		directory,
		interruptedRebuildDocuments{document: documents[0]},
	)
	if !errors.Is(err, errRebuildInterrupted) || index != nil {
		t.Fatalf("interrupted rebuild index=%v error=%v", index, err)
	}
	if _, err := os.Stat(bleveRebuildStatePath(path)); err != nil {
		t.Fatalf("rebuild state: %v", err)
	}

	stored := &fakeStoredDocuments{documents: documents}
	index, err = NewBleveDiskIndex(t.Context(), path, directory, stored)
	if err != nil {
		t.Fatalf("restart rebuild: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })
	if stored.scans != 1 {
		t.Fatalf("restart scans = %d, want 1", stored.scans)
	}
	if _, err := os.Stat(bleveRebuildStatePath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed rebuild state: %v", err)
	}
	for _, query := range []string{"first", "second"} {
		results, err := index.Search(t.Context(), SearchRequest{
			Query: query, Terms: []string{query}, MaxResults: 5,
		})
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		if len(results.Results) != 1 {
			t.Fatalf("search %q results = %#v", query, results.Results)
		}
	}
}

func TestBleveRebuildStateLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "search.bleve") + string(os.PathSeparator)
	if bleveRebuildStatePath(root) != filepath.Clean(root)+bleveRebuildStateSuffix {
		t.Fatalf("rebuild state path = %q", bleveRebuildStatePath(root))
	}
	pending, err := bleveRebuildPending(root)
	if err != nil || pending {
		t.Fatalf("initial state pending=%v error=%v", pending, err)
	}
	if err := requireBleveRebuild(root); err != nil {
		t.Fatalf("require rebuild: %v", err)
	}
	pending, err = bleveRebuildPending(root)
	if err != nil || !pending {
		t.Fatalf("required state pending=%v error=%v", pending, err)
	}
	if err := completeBleveRebuild(root); err != nil {
		t.Fatalf("complete rebuild: %v", err)
	}
	pending, err = bleveRebuildPending(root)
	if err != nil || pending {
		t.Fatalf("completed state pending=%v error=%v", pending, err)
	}
}

func TestBleveRebuildStateIOFailures(t *testing.T) {
	originalStat := statBleveRebuildState
	originalWrite := writeBleveRebuildState
	originalRemoveState := removeBleveRebuildState
	t.Cleanup(func() {
		statBleveRebuildState = originalStat
		writeBleveRebuildState = originalWrite
		removeBleveRebuildState = originalRemoveState
	})

	want := errors.New("state failure")
	statBleveRebuildState = func(string) (os.FileInfo, error) { return nil, want }
	if _, _, _, err := openOrCreateBleveDisk(t.TempDir(), true); !errors.Is(err, want) {
		t.Fatalf("inspect state error = %v", err)
	}
	statBleveRebuildState = originalStat

	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireBleveRebuild(filepath.Join(blocked, "search.bleve")); err == nil {
		t.Fatal("blocked state directory succeeded")
	}

	writeBleveRebuildState = func(string, []byte, os.FileMode) error { return want }
	if _, _, _, err := openOrCreateBleveDisk(
		writeLegacySearchIndex(t),
		true,
	); !errors.Is(err, want) {
		t.Fatalf("legacy persist state error = %v", err)
	}
	if _, _, _, err := openOrCreateBleveDisk(
		filepath.Join(t.TempDir(), "search.bleve"),
		true,
	); !errors.Is(err, want) {
		t.Fatalf("persist state error = %v", err)
	}
	writeBleveRebuildState = originalWrite

	removeBleveRebuildState = func(string) error { return want }
	root := filepath.Join(t.TempDir(), "search.bleve")
	index, err := NewBleveDiskIndex(
		t.Context(),
		root,
		newFakeDocumentDirectory(),
		&fakeStoredDocuments{},
	)
	if !errors.Is(err, want) || index != nil {
		t.Fatalf("complete state index=%v error=%v", index, err)
	}
}

func TestBlevePendingRebuildFailures(t *testing.T) {
	originalRemoveIndex := removeBleveDisk
	t.Cleanup(func() { removeBleveDisk = originalRemoveIndex })
	want := errors.New("state failure")

	pendingRoot := filepath.Join(t.TempDir(), "search.bleve")
	if err := requireBleveRebuild(pendingRoot); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := openOrCreateBleveDisk(pendingRoot, false); err == nil {
		t.Fatal("pending rebuild opened without documents")
	}
	removeBleveDisk = func(string) error { return want }
	if _, _, _, err := openOrCreateBleveDisk(pendingRoot, true); !errors.Is(err, want) {
		t.Fatalf("restart remove error = %v", err)
	}
}

func TestBleveShardRebuildStateFailures(t *testing.T) {
	want := errors.New("state failure")

	missingShard := filepath.Join(t.TempDir(), "missing.idx")
	if _, _, _, err := openOrCreateBleveShardForRebuild(
		missingShard,
		true,
		func() error { return want },
	); !errors.Is(err, want) {
		t.Fatalf("missing shard state error = %v", err)
	}

	staleShard := filepath.Join(t.TempDir(), "stale.idx")
	stale, err := bleve.New(staleShard, bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := openOrCreateBleveShardForRebuild(
		staleShard,
		true,
		func() error { return want },
	); !errors.Is(err, want) {
		t.Fatalf("stale shard state error = %v", err)
	}
}
