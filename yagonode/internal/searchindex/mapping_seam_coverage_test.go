package searchindex

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2/mapping"
)

func TestNewSearchIndexMappingPropagatesRegisterError(t *testing.T) {
	old := registerURLAnalyzer
	t.Cleanup(func() { registerURLAnalyzer = old })
	sentinel := errors.New("register failed")
	registerURLAnalyzer = func(*mapping.IndexMappingImpl) error { return sentinel }

	if _, err := newSearchIndexMapping(); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestNewBleveMemoryIndexMappingError(t *testing.T) {
	old := newSearchIndexMapping
	t.Cleanup(func() { newSearchIndexMapping = old })
	sentinel := errors.New("mapping failed")
	newSearchIndexMapping = func() (*mapping.IndexMappingImpl, error) { return nil, sentinel }

	if _, err := NewBleveMemoryIndex(t.Context(), nil); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestNewBleveDiskIndexCreateMappingError(t *testing.T) {
	old := newSearchIndexMapping
	t.Cleanup(func() { newSearchIndexMapping = old })
	sentinel := errors.New("mapping failed")
	newSearchIndexMapping = func() (*mapping.IndexMappingImpl, error) { return nil, sentinel }

	if _, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		newFakeDocumentDirectory(),
		nil,
	); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}

func TestNewBleveDiskIndexRecreateMappingError(t *testing.T) {
	sentinel := errors.New("mapping failed")
	oldMapping := newSearchIndexMapping
	t.Cleanup(func() { newSearchIndexMapping = oldMapping })
	newSearchIndexMapping = func() (*mapping.IndexMappingImpl, error) { return nil, sentinel }

	if _, err := NewBleveDiskIndex(
		t.Context(),
		filepath.Join(t.TempDir(), "search.bleve"),
		newFakeDocumentDirectory(),
		nil,
	); !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
}
