package searchindex

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestCachedMemoryIndexClosesItsBackend(t *testing.T) {
	index, err := NewBleveMemoryIndex(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cached := NewCachedSearchIndex(index, 2)
	if err := cached.Index(
		t.Context(),
		documentstore.Document{NormalizedURL: "https://example.org/page", Title: "needle"},
	); err != nil {
		t.Fatal(err)
	}
	if err := cached.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cached.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Search(
		t.Context(),
		SearchRequest{Query: "needle", MaxResults: 1},
	); err == nil {
		t.Fatal("closed backend accepted a search")
	}
}

func TestMemoryIndexPreservesCloseFailure(t *testing.T) {
	want := errors.New("close failed")
	index := &BleveMemoryIndex{index: closeErrorBleveIndex{err: want}}
	if err := index.Close(); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
}
