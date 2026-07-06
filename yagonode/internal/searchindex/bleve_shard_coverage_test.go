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

func TestShardOpenAndSearchFailureBranches(t *testing.T) {
	// A failed alias search surfaces: search with a closed alias.
	root := filepath.Join(t.TempDir(), "search.bleve")
	index, err := NewBleveDiskIndex(t.Context(), root, newFakeDocumentDirectory(), nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	// Populate then close the shards behind the alias's back.
	closeBleveShards(index.shards)
	// docCount fails first; force the alias path by stubbing counts.
	if _, err := index.docCount(); err == nil {
		t.Fatal("closed shard count must fail")
	}

	// An existing per-shard directory that fails to open without a rebuild
	// source surfaces the open error.
	badRoot := filepath.Join(t.TempDir(), "bad")
	shardDir := diskShardPath(badRoot, 0)
	if err := os.MkdirAll(shardDir, 0o750); err != nil {
		t.Fatalf("mk: %v", err)
	}
	oldOpen := openBleveDisk
	t.Cleanup(func() { openBleveDisk = oldOpen })
	openBleveDisk = func(string) (bleve.Index, error) { return nil, errors.New("corrupt shard") }
	if _, _, _, err := openOrCreateBleveShard(shardDir, false); err == nil {
		t.Fatal("unreadable shard without rebuild must fail")
	}
	openBleveDisk = oldOpen

	// A shard remove failure during repair surfaces.
	oldRemove := removeBleveDisk
	t.Cleanup(func() { removeBleveDisk = oldRemove })
	openBleveDisk = func(string) (bleve.Index, error) { return nil, errors.New("corrupt shard") }
	removeBleveDisk = func(string) error { return errors.New("remove failed") }
	if _, _, _, err := openOrCreateBleveShard(shardDir, true); err == nil {
		t.Fatal("failed shard removal must surface")
	}
	openBleveDisk = oldOpen
	removeBleveDisk = oldRemove

	// A file occupying a fanout directory fails shard creation.
	occupied := filepath.Join(t.TempDir(), "occ")
	if err := os.MkdirAll(occupied, 0o750); err != nil {
		t.Fatalf("mk: %v", err)
	}
	if err := os.WriteFile(filepath.Join(occupied, "00"), []byte("f"), 0o600); err != nil {
		t.Fatalf("occupy: %v", err)
	}
	if _, _, _, err := openOrCreateBleveShard(diskShardPath(occupied, 0), true); err == nil {
		t.Fatal("occupied fanout dir must fail")
	}

	// A read-only root fails the fanout MkdirAll.
	ro := filepath.Join(t.TempDir(), "ro")
	if err := os.MkdirAll(ro, 0o500); err != nil {
		t.Fatalf("mk ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) }) //nolint:gosec // test cleanup
	if _, _, _, err := openOrCreateBleveShard(diskShardPath(ro, 0), true); err == nil {
		t.Fatal("read-only root must fail shard creation")
	}
}

func TestAliasSearchErrorSurfaces(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	doc := documentstore.Document{
		NormalizedURL: "https://example.org/needle",
		ExtractedText: "needle text",
	}
	index, err := NewBleveDiskIndex(
		t.Context(), root,
		newFakeDocumentDirectory(doc), nil,
	)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := index.Index(t.Context(), doc); err != nil {
		t.Fatalf("index: %v", err)
	}
	// Swap the alias for a stub whose search fails after the count succeeds.
	index.alias = searchErrorBleveIndex{err: errors.New("search boom")}
	if _, err := index.Search(
		t.Context(), SearchRequest{Query: "needle", MaxResults: 1},
	); err == nil {
		t.Fatal("alias search failure must surface")
	}
	index.alias = bleve.NewIndexAlias(index.shards...)
	if err := index.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// searchErrorBleveIndex fails every search; the rest of the contract is inert.
type searchErrorBleveIndex struct {
	bleveIndexContract
	err error
}

func (i searchErrorBleveIndex) SearchInContext(
	context.Context,
	*bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	return nil, i.err
}

func TestLegacyRetireFailureAndPreGramShard(t *testing.T) {
	// A legacy layout whose removal fails surfaces the retire error.
	legacyRoot := filepath.Join(t.TempDir(), "legacy.bleve")
	if err := os.WriteFile(legacyRoot, []byte("old"), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	oldRemove := removeBleveDisk
	t.Cleanup(func() { removeBleveDisk = oldRemove })
	sentinel := errors.New("retire failed")
	removeBleveDisk = func(string) error { return sentinel }
	if _, err := NewBleveDiskIndex(
		t.Context(), legacyRoot,
		newFakeDocumentDirectory(),
		&fakeStoredDocuments{},
	); !errors.Is(err, sentinel) {
		t.Fatalf("retire error = %v, want %v", err, sentinel)
	}
	removeBleveDisk = oldRemove

	// A pre-gram shard inside the sharded layout recreates on open.
	root := filepath.Join(t.TempDir(), "sharded.bleve")
	shardPath := diskShardPath(root, 0)
	if err := os.MkdirAll(filepath.Dir(shardPath), 0o750); err != nil {
		t.Fatalf("mk: %v", err)
	}
	preGram, err := bleve.New(shardPath, bleve.NewIndexMapping())
	if err != nil {
		t.Fatalf("pre-gram shard: %v", err)
	}
	if err := preGram.Close(); err != nil {
		t.Fatalf("close pre-gram: %v", err)
	}
	index, err := NewBleveDiskIndex(
		t.Context(), root,
		newFakeDocumentDirectory(),
		&fakeStoredDocuments{},
	)
	if err != nil {
		t.Fatalf("recreate pre-gram shard: %v", err)
	}
	if !index.gram {
		t.Fatal("recreated shards must carry the gram mapping")
	}
	if err := index.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
