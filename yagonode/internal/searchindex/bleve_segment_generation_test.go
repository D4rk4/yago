package searchindex

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBleveSegmentGenerationState(t *testing.T) {
	directory := t.TempDir()
	root := filepath.Join(directory, "search.bleve") + string(os.PathSeparator)
	wantPath := filepath.Clean(root) + bleveSegmentGenerationSuffix
	if bleveSegmentGenerationPath(root) != wantPath {
		t.Fatalf("generation path = %q, want %q", bleveSegmentGenerationPath(root), wantPath)
	}
	current, err := bleveSegmentGenerationIsCurrent(root)
	if err != nil || current {
		t.Fatalf("missing generation current=%v error=%v", current, err)
	}
	if err := os.WriteFile(wantPath, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err = bleveSegmentGenerationIsCurrent(root)
	if err != nil || current {
		t.Fatalf("old generation current=%v error=%v", current, err)
	}
	if err := persistCurrentBleveSegmentGeneration(root); err != nil {
		t.Fatal(err)
	}
	current, err = bleveSegmentGenerationIsCurrent(root)
	if err != nil || !current {
		t.Fatalf("persisted generation current=%v error=%v", current, err)
	}
	directoryRoot, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = directoryRoot.Close() })
	value, err := directoryRoot.ReadFile(filepath.Base(wantPath))
	if err != nil || !bytes.Equal(value, bleveSegmentGeneration) {
		t.Fatalf("generation value=%q error=%v", value, err)
	}
	info, err := os.Stat(wantPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("generation mode=%v error=%v", info.Mode().Perm(), err)
	}
}

func TestBleveSegmentGenerationRejectsIOFailures(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	if err := os.Mkdir(bleveSegmentGenerationPath(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := bleveSegmentGenerationIsCurrent(root); err == nil {
		t.Fatal("generation directory was read as a generation file")
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := persistCurrentBleveSegmentGeneration(filepath.Join(blocked, "search")); err == nil {
		t.Fatal("generation was persisted below a file")
	}
}

func TestBleveIndexRootPresence(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	present, err := bleveIndexRootPresent(missing)
	if err != nil || present {
		t.Fatalf("missing root present=%v error=%v", present, err)
	}
	empty := t.TempDir()
	present, err = bleveIndexRootPresent(empty)
	if err != nil || present {
		t.Fatalf("empty root present=%v error=%v", present, err)
	}
	fileRoot := filepath.Join(t.TempDir(), "index")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	present, err = bleveIndexRootPresent(fileRoot)
	if err != nil || !present {
		t.Fatalf("file root present=%v error=%v", present, err)
	}
	legacyRoot := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(legacyRoot, "index_meta.json"),
		[]byte("{}"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	present, err = bleveIndexRootPresent(legacyRoot)
	if err != nil || !present {
		t.Fatalf("legacy root present=%v error=%v", present, err)
	}
	shardedRoot := t.TempDir()
	if err := os.MkdirAll(diskShardPath(shardedRoot, diskShardCount-1), 0o700); err != nil {
		t.Fatal(err)
	}
	present, err = bleveIndexRootPresent(shardedRoot)
	if err != nil || !present {
		t.Fatalf("sharded root present=%v error=%v", present, err)
	}
	if _, err := bleveIndexRootPresent(string([]byte{'x', 0})); err == nil {
		t.Fatal("invalid root path succeeded")
	}
}

func TestBleveIndexRootPresenceRejectsMetadataAndShardStatFailures(t *testing.T) {
	metadataRoot := t.TempDir()
	metadataPath := filepath.Join(metadataRoot, "index_meta.json")
	if err := os.Symlink(metadataPath, metadataPath); err != nil {
		t.Fatal(err)
	}
	if _, err := bleveIndexRootPresent(metadataRoot); err == nil {
		t.Fatal("metadata symlink loop succeeded")
	}

	shardRoot := t.TempDir()
	shardPath := diskShardPath(shardRoot, 0)
	if err := os.MkdirAll(filepath.Dir(shardPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shardPath, shardPath); err != nil {
		t.Fatal(err)
	}
	if _, err := bleveIndexRootPresent(shardRoot); err == nil {
		t.Fatal("shard symlink loop succeeded")
	}
}

func TestBleveSegmentGenerationAdmission(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if err := requireCurrentBleveSegmentGeneration(missing, false); err != nil {
		t.Fatalf("missing root: %v", err)
	}
	empty := t.TempDir()
	if err := requireCurrentBleveSegmentGeneration(empty, false); err != nil {
		t.Fatalf("empty root: %v", err)
	}

	withoutSource := filepath.Join(t.TempDir(), "old.idx")
	if err := os.WriteFile(withoutSource, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireCurrentBleveSegmentGeneration(withoutSource, false); err == nil {
		t.Fatal("old generation without documents was admitted")
	}
	if _, err := os.Stat(bleveRebuildStatePath(withoutSource)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused index changed rebuild state: %v", err)
	}

	withSourceDirectory := t.TempDir()
	withSource := filepath.Join(withSourceDirectory, "old.idx")
	if err := os.WriteFile(withSource, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireCurrentBleveSegmentGeneration(withSource, true); err != nil {
		t.Fatalf("old generation with documents: %v", err)
	}
	withSourceRoot, err := os.OpenRoot(withSourceDirectory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = withSourceRoot.Close() })
	value, err := withSourceRoot.ReadFile(filepath.Base(bleveRebuildStatePath(withSource)))
	if err != nil || string(value) != "required\n" {
		t.Fatalf("rebuild state=%q error=%v", value, err)
	}

	current := filepath.Join(t.TempDir(), "current.idx")
	if err := os.WriteFile(current, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := persistCurrentBleveSegmentGeneration(current); err != nil {
		t.Fatal(err)
	}
	if err := requireCurrentBleveSegmentGeneration(current, false); err != nil {
		t.Fatalf("current generation: %v", err)
	}
	if _, err := os.Stat(bleveRebuildStatePath(current)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("current index changed rebuild state: %v", err)
	}
}

func requireBleveSegmentGenerationCurrent(t *testing.T, root string) {
	t.Helper()
	current, err := bleveSegmentGenerationIsCurrent(root)
	if err != nil || !current {
		t.Fatalf("segment generation current=%v error=%v", current, err)
	}
}

func TestBleveSegmentGenerationAdmissionRejectsGenerationReadFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	if err := os.WriteFile(root, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bleveSegmentGenerationPath(root), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := requireCurrentBleveSegmentGeneration(root, true); err == nil {
		t.Fatal("unreadable segment generation was admitted")
	}
	if _, err := os.Stat(bleveRebuildStatePath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read failure changed rebuild state: %v", err)
	}
}

func TestBleveSegmentGenerationAdmissionRejectsRebuildStateFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	if err := os.WriteFile(root, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := errors.New("state failed")
	original := writeBleveRebuildState
	t.Cleanup(func() { writeBleveRebuildState = original })
	writeBleveRebuildState = func(string, []byte, os.FileMode) error { return want }
	if err := requireCurrentBleveSegmentGeneration(root, true); !errors.Is(err, want) {
		t.Fatalf("rebuild state error = %v, want %v", err, want)
	}
}
