package searchindex

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestBleveCompatibleSegmentGenerationAdvancesWithoutRebuild(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	if err := os.WriteFile(root, []byte("index"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		bleveSegmentGenerationPath(root),
		bleveCompatibleSegmentGeneration,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := requireCurrentBleveSegmentGeneration(root, false); err != nil {
		t.Fatalf("compatible generation: %v", err)
	}
	value, err := os.ReadFile(bleveSegmentGenerationPath(root))
	if err != nil || !bytes.Equal(value, bleveSegmentGeneration) {
		t.Fatalf("advanced generation=%q error=%v", value, err)
	}
	if _, err := os.Stat(bleveRebuildStatePath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("compatible generation scheduled rebuild: %v", err)
	}
}

func TestBleveCurrentSegmentGenerationRemainsUnchanged(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	if err := os.WriteFile(root, []byte("index"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := persistCurrentBleveSegmentGeneration(root); err != nil {
		t.Fatal(err)
	}
	originalWrite := writeBleveSegmentGeneration
	t.Cleanup(func() { writeBleveSegmentGeneration = originalWrite })
	writes := 0
	writeBleveSegmentGeneration = func(string, []byte, os.FileMode) error {
		writes++

		return errors.New("current generation rewritten")
	}
	if err := requireCurrentBleveSegmentGeneration(root, false); err != nil {
		t.Fatalf("current generation: %v", err)
	}
	if writes != 0 {
		t.Fatalf("current generation writes=%d", writes)
	}
}

func TestBleveCompatibleSegmentGenerationRejectsAdvanceFailure(t *testing.T) {
	root := filepath.Join(t.TempDir(), "search.bleve")
	if err := os.WriteFile(root, []byte("index"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		bleveSegmentGenerationPath(root),
		bleveCompatibleSegmentGeneration,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	want := errors.New("generation advance failed")
	originalWrite := writeBleveSegmentGeneration
	t.Cleanup(func() { writeBleveSegmentGeneration = originalWrite })
	writeBleveSegmentGeneration = func(string, []byte, os.FileMode) error { return want }
	if err := requireCurrentBleveSegmentGeneration(root, true); !errors.Is(err, want) {
		t.Fatalf("advance error=%v, want=%v", err, want)
	}
	value, err := os.ReadFile(bleveSegmentGenerationPath(root))
	if err != nil || !bytes.Equal(value, bleveCompatibleSegmentGeneration) {
		t.Fatalf("failed advance generation=%q error=%v", value, err)
	}
	if _, err := os.Stat(bleveRebuildStatePath(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed advance scheduled rebuild: %v", err)
	}
}
