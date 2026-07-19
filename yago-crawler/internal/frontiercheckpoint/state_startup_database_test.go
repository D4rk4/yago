package frontiercheckpoint

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestOpenFrontierStateDatabaseOpensBeforeStartupLeaseRelease(t *testing.T) {
	path := testCheckpointPath(t)
	if err := os.MkdirAll(filepath.Dir(path), privateDirectoryMode()); err != nil {
		t.Fatalf("create checkpoint directory: %v", err)
	}
	startupActive := false
	openedWhileActive := false
	runStartup := func(_ string, timeout time.Duration, operation func() error) error {
		if timeout != databaseLockTimeout {
			t.Fatalf("startup timeout = %s", timeout)
		}
		startupActive = true
		err := operation()
		startupActive = false

		return err
	}
	openDatabase := func(
		path string,
		mode os.FileMode,
		options *bolt.Options,
	) (*bolt.DB, error) {
		openedWhileActive = startupActive

		return bolt.Open(path, mode, options)
	}
	database, err := openFrontierStateDatabaseWithStartup(
		path,
		0,
		nil,
		runStartup,
		openDatabase,
	)
	if err != nil {
		t.Fatalf("open frontier state database: %v", err)
	}
	if !openedWhileActive {
		t.Fatal("authoritative database opened after startup lease release")
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close frontier state database: %v", err)
	}
}

func TestOpenFrontierStateDatabaseClosesHandleAfterLeaseReleaseFailure(t *testing.T) {
	path := testCheckpointPath(t)
	if err := os.MkdirAll(filepath.Dir(path), privateDirectoryMode()); err != nil {
		t.Fatalf("create checkpoint directory: %v", err)
	}
	want := errors.New("release failed")
	runStartup := func(_ string, _ time.Duration, operation func() error) error {
		if err := operation(); err != nil {
			return err
		}

		return want
	}
	database, err := openFrontierStateDatabaseWithStartup(
		path,
		0,
		nil,
		runStartup,
		bolt.Open,
	)
	if database != nil || !errors.Is(err, want) {
		t.Fatalf("database after release failure = %v, %v", database, err)
	}
	reopened, err := bolt.Open(
		path,
		frontierStateFileMode,
		&bolt.Options{Timeout: databaseLockTimeout},
	)
	if err != nil {
		t.Fatalf("authoritative database handle leaked: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened database: %v", err)
	}
}

func TestOpenFrontierStateDatabaseReportsOpenFailure(t *testing.T) {
	want := errors.New("open failed")
	runStartup := func(_ string, _ time.Duration, operation func() error) error {
		return operation()
	}
	openDatabase := func(
		string,
		os.FileMode,
		*bolt.Options,
	) (*bolt.DB, error) {
		return nil, want
	}
	database, err := openFrontierStateDatabaseWithStartup(
		"frontier.db",
		0,
		nil,
		runStartup,
		openDatabase,
	)
	if database != nil || !errors.Is(err, want) {
		t.Fatalf("database open failure = %v, %v", database, err)
	}
}

func TestOpenWithStateMaximumFailsBeforeMaintenanceWhenStartupLeaseIsHeld(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint fixture: %v", err)
	}
	lease, err := acquireFrontierStateStartupLease(path, time.Second)
	if err != nil {
		t.Fatalf("acquire blocking startup lease: %v", err)
	}
	defer func() { _ = lease.release() }()
	maintenance := &frontierStateMaintenanceProbe{}
	opened, err := OpenWithStateMaximum(path, 1, maintenance)
	if opened != nil || !errors.Is(err, errFrontierStateStartupLeaseTimeout) {
		t.Fatalf("contended checkpoint open = %v, %v", opened, err)
	}
	if maintenance.calls != 0 {
		t.Fatalf("maintenance calls before startup lease = %d", maintenance.calls)
	}
}

type frontierStateMaintenanceProbe struct {
	calls int
}

func (probe *frontierStateMaintenanceProbe) RunMaintenanceWithHeadroom(
	measure func() (uint64, error),
	operation func(uint64) error,
) error {
	probe.calls++
	requiredBytes, err := measure()
	if err != nil {
		return err
	}

	return operation(requiredBytes)
}
