package boltvault

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestCopyCompactedFilePreservesRowsAndSecuresTarget(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	source := openCompactedFileCopyTestDatabase(t, sourcePath)
	if err := source.Update(func(transaction *bolt.Tx) error {
		bucket, err := transaction.CreateBucketIfNotExists([]byte("state"))
		if err != nil {
			return fmt.Errorf("create compacted-file bucket: %w", err)
		}
		if err := bucket.Put([]byte("retained"), []byte("live")); err != nil {
			return fmt.Errorf("write compacted-file row: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("write compacted-file source: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close compacted-file source: %v", err)
	}
	destinationPath := filepath.Join(t.TempDir(), "destination.db")
	if err := CopyCompactedFile(sourcePath, destinationPath, time.Second); err != nil {
		t.Fatalf("copy compacted file: %v", err)
	}
	info, err := os.Stat(destinationPath)
	if err != nil {
		t.Fatalf("inspect compacted-file target: %v", err)
	}
	if info.Mode().Perm() != compactedFileMode {
		t.Fatalf("compacted-file target mode = %o", info.Mode().Perm())
	}
	destination := openCompactedFileCopyTestDatabase(t, destinationPath)
	defer func() { _ = destination.Close() }()
	if err := destination.View(func(transaction *bolt.Tx) error {
		value := transaction.Bucket([]byte("state")).Get([]byte("retained"))
		if !bytes.Equal(value, []byte("live")) {
			t.Fatalf("retained compacted-file value = %q", value)
		}

		return nil
	}); err != nil {
		t.Fatalf("read compacted-file target: %v", err)
	}
}

func TestCopyCompactedFileReportsOpenFailures(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		if err := CopyCompactedFile("source.db", "destination.db", timeout); err == nil {
			t.Fatalf("compacted-file copy accepted timeout %v", timeout)
		}
	}
	invalidPath := filepath.Join(t.TempDir(), "invalid.db")
	if err := os.WriteFile(invalidPath, []byte("invalid"), 0o600); err != nil {
		t.Fatalf("write invalid compacted-file source: %v", err)
	}
	if err := CopyCompactedFile(invalidPath, invalidPath+".copy", time.Second); err == nil {
		t.Fatal("invalid compacted-file source opened")
	}
	sourcePath := filepath.Join(t.TempDir(), "source.db")
	source := openCompactedFileCopyTestDatabase(t, sourcePath)
	if err := source.Close(); err != nil {
		t.Fatalf("close compacted-file source: %v", err)
	}
	if err := CopyCompactedFile(
		sourcePath,
		filepath.Join(t.TempDir(), "missing", "destination.db"),
		time.Second,
	); err == nil {
		t.Fatal("missing compacted-file target directory succeeded")
	}
}

func TestFinishCompactedFileCopyJoinsFailures(t *testing.T) {
	source := openCompactedFileCopyTestDatabase(t, filepath.Join(t.TempDir(), "source.db"))
	destinationPath := filepath.Join(t.TempDir(), "destination.db")
	destination := openCompactedFileCopyTestDatabase(t, destinationPath)
	want := errors.New("compact failed")
	if err := finishCompactedFileCopy(
		destination,
		source,
		destinationPath,
		func(*bolt.DB, *bolt.DB, int64) error { return want },
		os.Chmod,
	); !errors.Is(err, want) {
		t.Fatalf("compacted-file copy failure = %v", err)
	}
}

func openCompactedFileCopyTestDatabase(t *testing.T, path string) *bolt.DB {
	t.Helper()
	database, err := bolt.Open(path, compactedFileMode, nil)
	if err != nil {
		t.Fatalf("open compacted-file test database: %v", err)
	}

	return database
}
