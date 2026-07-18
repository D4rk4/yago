package frontiercheckpoint

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenSecuresDatabaseAndParent(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	if checkpoint.database.NoSync {
		t.Fatal("bbolt NoSync must remain disabled")
	}
	directoryInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat checkpoint directory: %v", err)
	}
	if directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %o, want 700", directoryInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat checkpoint file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %o, want 600", fileInfo.Mode().Perm())
	}
}

func TestWorkerIdentityIsStableAndDatabaseSpecific(t *testing.T) {
	firstPath := testCheckpointPath(t)
	first := openTestCheckpoint(t, firstPath)
	firstID, err := first.WorkerID("crawler")
	if err != nil {
		t.Fatalf("first worker identity: %v", err)
	}
	repeatedID, err := first.WorkerID("crawler")
	if err != nil || repeatedID != firstID {
		t.Fatalf("repeated worker identity = %q, %v, want %q", repeatedID, err, firstID)
	}
	otherPrefixID, err := first.WorkerID("worker")
	if err != nil || otherPrefixID != firstID {
		t.Fatalf("other prefix identity = %q, %v, want %q", otherPrefixID, err, firstID)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}
	reopened := openTestCheckpoint(t, firstPath)
	reopenedID, err := reopened.WorkerID("crawler")
	if err != nil || reopenedID != firstID {
		t.Fatalf("reopened worker identity = %q, %v, want %q", reopenedID, err, firstID)
	}
	second := openTestCheckpoint(t, testCheckpointPath(t))
	secondID, err := second.WorkerID("crawler")
	if err != nil {
		t.Fatalf("second worker identity: %v", err)
	}
	if workerSuffix(secondID, "crawler") == workerSuffix(firstID, "crawler") {
		t.Fatalf("independent databases share worker suffix %q", workerSuffix(firstID, "crawler"))
	}
}

func TestOpenFailsWhileDatabaseLockIsHeld(t *testing.T) {
	path := testCheckpointPath(t)
	first := openTestCheckpoint(t, path)
	started := time.Now()
	second, err := Open(path)
	if err == nil {
		_ = second.Close()
		t.Fatal("second open unexpectedly acquired database lock")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("lock failure took %v", time.Since(started))
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}
	third := openTestCheckpoint(t, path)
	if third == nil {
		t.Fatal("database did not reopen after lock release")
	}
}

func TestOpenRejectsInvalidPaths(t *testing.T) {
	if _, err := Open(""); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("empty path error = %v", err)
	}
	parentFile := filepath.Join(t.TempDir(), "parent-file")
	if err := os.WriteFile(parentFile, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create parent file: %v", err)
	}
	if _, err := Open(filepath.Join(parentFile, "frontier.db")); err == nil {
		t.Fatal("checkpoint opened beneath a regular file")
	}
	directoryPath := filepath.Join(t.TempDir(), "database-directory")
	if err := os.MkdirAll(directoryPath, 0o700); err != nil {
		t.Fatalf("create database directory: %v", err)
	}
	if _, err := Open(directoryPath); err == nil {
		t.Fatal("checkpoint opened a directory as a database file")
	}
}

func TestWorkerIdentityRejectsBlankPrefix(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if _, err := checkpoint.WorkerID(" \t"); !errors.Is(err, ErrInvalidWorkerPrefix) {
		t.Fatalf("blank worker prefix error = %v", err)
	}
	if workerID, err := checkpoint.WorkerID("crawler"); err != nil ||
		!strings.HasPrefix(workerID, "crawler-") {
		t.Fatalf("worker identity = %q, %v", workerID, err)
	}
}
