package frontiercheckpoint

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestFrontierStateStartupLeasePersistsAndSerializesConcurrentStarts(t *testing.T) {
	path := testCheckpointPath(t)
	if err := os.MkdirAll(filepath.Dir(path), privateDirectoryMode()); err != nil {
		t.Fatalf("create checkpoint directory: %v", err)
	}
	first, err := acquireFrontierStateStartupLease(path, time.Second)
	if err != nil {
		t.Fatalf("acquire first startup lease: %v", err)
	}
	firstReleased := false
	t.Cleanup(func() {
		if !firstReleased {
			_ = first.release()
		}
	})
	leasePath := path + frontierStateStartupLeaseSuffix
	before, err := os.Stat(leasePath)
	if err != nil {
		t.Fatalf("stat startup lease: %v", err)
	}
	if before.Mode().Perm() != frontierStateFileMode {
		t.Fatalf("startup lease mode = %o", before.Mode().Perm())
	}

	system := defaultFrontierStateStartupLeaseSystem()
	lock := system.lock
	attempted := make(chan struct{})
	var attempt sync.Once
	system.lock = func(descriptor int, operation int) error {
		if operation == syscall.LOCK_EX|syscall.LOCK_NB {
			attempt.Do(func() { close(attempted) })
		}

		return lock(descriptor, operation)
	}
	type acquisition struct {
		lease *frontierStateStartupLease
		err   error
	}
	secondDone := make(chan acquisition, 1)
	go func() {
		lease, acquireErr := acquireFrontierStateStartupLeaseWithSystem(
			path,
			time.Second,
			system,
		)
		secondDone <- acquisition{lease: lease, err: acquireErr}
	}()
	<-attempted
	select {
	case result := <-secondDone:
		if result.lease != nil {
			_ = result.lease.release()
		}
		t.Fatalf("second startup entered while first held lease: %v", result.err)
	default:
	}
	if err := first.release(); err != nil {
		t.Fatalf("release first startup lease: %v", err)
	}
	firstReleased = true
	second := <-secondDone
	if second.err != nil {
		t.Fatalf("acquire serialized startup lease: %v", second.err)
	}
	if err := second.lease.release(); err != nil {
		t.Fatalf("release second startup lease: %v", err)
	}
	after, err := os.Stat(leasePath)
	if err != nil {
		t.Fatalf("stat persistent startup lease: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("startup lease file was replaced")
	}
}

func TestFrontierStateStartupLeaseTimeoutKeepsLeaseFile(t *testing.T) {
	path := testCheckpointPath(t)
	if err := os.MkdirAll(filepath.Dir(path), privateDirectoryMode()); err != nil {
		t.Fatalf("create checkpoint directory: %v", err)
	}
	first, err := acquireFrontierStateStartupLease(path, time.Second)
	if err != nil {
		t.Fatalf("acquire first startup lease: %v", err)
	}
	defer func() { _ = first.release() }()
	leasePath := path + frontierStateStartupLeaseSuffix
	before, err := os.Stat(leasePath)
	if err != nil {
		t.Fatalf("stat startup lease: %v", err)
	}
	second, err := acquireFrontierStateStartupLease(path, 20*time.Millisecond)
	if second != nil || !errors.Is(err, errFrontierStateStartupLeaseTimeout) {
		t.Fatalf("contended startup lease = %v, %v", second, err)
	}
	after, err := os.Stat(leasePath)
	if err != nil {
		t.Fatalf("stat timed-out startup lease: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("timed-out acquisition replaced the startup lease file")
	}
}

func TestFrontierStateStartupLeaseReportsSystemFailures(t *testing.T) {
	wantOpen := errors.New("open failed")
	system := defaultFrontierStateStartupLeaseSystem()
	system.open = func(string, int, os.FileMode) (*os.File, error) {
		return nil, wantOpen
	}
	if _, err := acquireFrontierStateStartupLeaseWithSystem(
		"frontier.db",
		time.Second,
		system,
	); !errors.Is(err, wantOpen) {
		t.Fatalf("startup lease open failure = %v", err)
	}
	if _, err := acquireFrontierStateStartupLeaseWithSystem(
		"frontier.db",
		0,
		system,
	); err == nil {
		t.Fatal("nonpositive startup lease timeout succeeded")
	}

	t.Run("secure and close", func(t *testing.T) {
		wantSecure := errors.New("secure failed")
		wantClose := errors.New("close failed")
		system := defaultFrontierStateStartupLeaseSystem()
		system.secure = func(*os.File, os.FileMode) error { return wantSecure }
		system.close = closeFrontierStateLeaseWithError(wantClose)
		if _, err := acquireFrontierStateStartupLeaseWithSystem(
			filepath.Join(t.TempDir(), "frontier.db"),
			time.Second,
			system,
		); !errors.Is(err, wantSecure) || !errors.Is(err, wantClose) {
			t.Fatalf("startup lease secure failure = %v", err)
		}
	})

	t.Run("lock and close", func(t *testing.T) {
		wantLock := errors.New("lock failed")
		wantClose := errors.New("close failed")
		system := defaultFrontierStateStartupLeaseSystem()
		system.lock = func(int, int) error { return wantLock }
		system.close = closeFrontierStateLeaseWithError(wantClose)
		if _, err := acquireFrontierStateStartupLeaseWithSystem(
			filepath.Join(t.TempDir(), "frontier.db"),
			time.Second,
			system,
		); !errors.Is(err, wantLock) || !errors.Is(err, wantClose) {
			t.Fatalf("startup lease lock failure = %v", err)
		}
	})
}

func TestFrontierStateStartupLeaseBoundsRetryAndReportsReleaseFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.db")
	clock := time.Unix(0, 0)
	var waits []time.Duration
	system := defaultFrontierStateStartupLeaseSystem()
	system.lock = func(int, int) error { return syscall.EWOULDBLOCK }
	system.now = func() time.Time { return clock }
	system.wait = func(duration time.Duration) {
		waits = append(waits, duration)
		clock = clock.Add(duration)
	}
	lease, err := acquireFrontierStateStartupLeaseWithSystem(
		path,
		15*time.Millisecond,
		system,
	)
	if lease != nil || !errors.Is(err, errFrontierStateStartupLeaseTimeout) {
		t.Fatalf("bounded startup lease retry = %v, %v", lease, err)
	}
	if len(waits) != 2 || waits[0] != 10*time.Millisecond || waits[1] != 5*time.Millisecond {
		t.Fatalf("startup lease waits = %v", waits)
	}

	wantUnlock := errors.New("unlock failed")
	wantClose := errors.New("close failed")
	file, err := os.CreateTemp(t.TempDir(), "frontier-*")
	if err != nil {
		t.Fatalf("open release fixture: %v", err)
	}
	if err := file.Chmod(frontierStateFileMode); err != nil {
		_ = file.Close()
		t.Fatalf("secure release fixture: %v", err)
	}
	releaseSystem := defaultFrontierStateStartupLeaseSystem()
	releaseSystem.lock = func(int, int) error { return wantUnlock }
	releaseSystem.close = closeFrontierStateLeaseWithError(wantClose)
	err = (&frontierStateStartupLease{file: file, system: releaseSystem}).release()
	if !errors.Is(err, wantUnlock) || !errors.Is(err, wantClose) {
		t.Fatalf("startup lease release failure = %v", err)
	}
}

func TestRunWithFrontierStateStartupLeaseReturnsOperationFailure(t *testing.T) {
	path := testCheckpointPath(t)
	if err := os.MkdirAll(filepath.Dir(path), privateDirectoryMode()); err != nil {
		t.Fatalf("create checkpoint directory: %v", err)
	}
	want := errors.New("operation failed")
	if err := runWithFrontierStateStartupLease(path, time.Second, func() error {
		return want
	}); !errors.Is(err, want) {
		t.Fatalf("startup operation failure = %v", err)
	}
	lease, err := acquireFrontierStateStartupLease(path, time.Second)
	if err != nil {
		t.Fatalf("reacquire startup lease: %v", err)
	}
	if err := lease.release(); err != nil {
		t.Fatalf("release reacquired startup lease: %v", err)
	}
}

func closeFrontierStateLeaseWithError(want error) func(*os.File) error {
	return func(file *os.File) error {
		return errors.Join(file.Close(), want)
	}
}
