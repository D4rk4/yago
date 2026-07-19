package boltvault

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDatabaseStartupLeaseSurvivesDatabaseReplacement(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(databasePath, []byte("old"), 0o600); err != nil {
		t.Fatalf("write old database fixture: %v", err)
	}
	first, err := AcquireDatabaseStartupLease(databasePath, time.Second)
	if err != nil {
		t.Fatalf("acquire first startup lease: %v", err)
	}
	replacement := databasePath + ".replacement"
	if err := os.WriteFile(replacement, []byte("new"), 0o600); err != nil {
		t.Fatalf("write replacement database fixture: %v", err)
	}
	if err := os.Rename(replacement, databasePath); err != nil {
		t.Fatalf("replace database fixture: %v", err)
	}
	second, err := AcquireDatabaseStartupLease(databasePath, 20*time.Millisecond)
	if second != nil || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("contending startup lease = %v, %v", second, err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release first startup lease: %v", err)
	}
	second, err = AcquireDatabaseStartupLease(databasePath, time.Second)
	if err != nil {
		t.Fatalf("acquire startup lease after handoff: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("release second startup lease: %v", err)
	}
	info, err := os.Stat(databasePath + databaseStartupLeaseSuffix)
	if err != nil {
		t.Fatalf("inspect persistent startup lease: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("startup lease mode = %o, want 600", info.Mode().Perm())
	}
}

func TestDatabaseStartupLeaseRejectsInvalidBounds(t *testing.T) {
	if lease, err := AcquireDatabaseStartupLease(" ", time.Second); lease != nil || err == nil {
		t.Fatalf("blank database path lease = %v, %v", lease, err)
	}
	if lease, err := AcquireDatabaseStartupLease("state.db", 0); lease != nil || err == nil {
		t.Fatalf("unbounded startup lease = %v, %v", lease, err)
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocked parent: %v", err)
	}
	if lease, err := AcquireDatabaseStartupLease(
		filepath.Join(blocked, "state.db"),
		time.Second,
	); lease != nil || err == nil {
		t.Fatalf("blocked startup lease directory = %v, %v", lease, err)
	}
}

func TestDatabaseStartupLeaseReportsBoundaryFailures(t *testing.T) {
	previousOpen := openDatabaseStartupLeaseFile
	previousSecure := secureDatabaseStartupLeaseFile
	previousTry := tryDatabaseStartupLease
	t.Cleanup(func() {
		openDatabaseStartupLeaseFile = previousOpen
		secureDatabaseStartupLeaseFile = previousSecure
		tryDatabaseStartupLease = previousTry
	})
	want := errors.New("startup lease failure")

	openDatabaseStartupLeaseFile = func(string, int, os.FileMode) (*os.File, error) {
		return nil, want
	}
	if lease, err := AcquireDatabaseStartupLease(
		filepath.Join(t.TempDir(), "open.db"),
		time.Second,
	); lease != nil || !errors.Is(err, want) {
		t.Fatalf("startup lease open failure = %v, %v", lease, err)
	}
	openDatabaseStartupLeaseFile = previousOpen

	secureDatabaseStartupLeaseFile = func(string, os.FileMode) error { return want }
	if lease, err := AcquireDatabaseStartupLease(
		filepath.Join(t.TempDir(), "secure.db"),
		time.Second,
	); lease != nil || !errors.Is(err, want) {
		t.Fatalf("startup lease secure failure = %v, %v", lease, err)
	}
	secureDatabaseStartupLeaseFile = previousSecure

	tryDatabaseStartupLease = func(*os.File) error { return want }
	if lease, err := AcquireDatabaseStartupLease(
		filepath.Join(t.TempDir(), "lock.db"),
		time.Second,
	); lease != nil || !errors.Is(err, want) {
		t.Fatalf("startup lease lock failure = %v, %v", lease, err)
	}
	tryDatabaseStartupLease = previousTry

	lease, err := AcquireDatabaseStartupLease(
		filepath.Join(t.TempDir(), "release.db"),
		time.Second,
	)
	if err != nil {
		t.Fatalf("acquire release fixture: %v", err)
	}
	if err := lease.file.Close(); err != nil {
		t.Fatalf("preclose startup lease: %v", err)
	}
	if err := lease.Release(); !errors.Is(err, syscall.EBADF) {
		t.Fatalf("startup lease release failure = %v", err)
	}
}
