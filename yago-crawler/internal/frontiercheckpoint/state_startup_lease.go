package frontiercheckpoint

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

const (
	frontierStateStartupLeaseSuffix = ".startup.lock"
	frontierStateStartupLeaseRetry  = 10 * time.Millisecond
)

var errFrontierStateStartupLeaseTimeout = errors.New("frontier state startup lease timeout")

type frontierStateStartupLeaseSystem struct {
	open   func(string, int, os.FileMode) (*os.File, error)
	secure func(*os.File, os.FileMode) error
	lock   func(int, int) error
	close  func(*os.File) error
	now    func() time.Time
	wait   func(time.Duration)
}

type frontierStateStartupLease struct {
	file   *os.File
	system frontierStateStartupLeaseSystem
}

func defaultFrontierStateStartupLeaseSystem() frontierStateStartupLeaseSystem {
	return frontierStateStartupLeaseSystem{
		open: os.OpenFile,
		secure: func(file *os.File, mode os.FileMode) error {
			return file.Chmod(mode)
		},
		lock:  syscall.Flock,
		close: func(file *os.File) error { return file.Close() },
		now:   time.Now,
		wait:  time.Sleep,
	}
}

func acquireFrontierStateStartupLease(
	path string,
	timeout time.Duration,
) (*frontierStateStartupLease, error) {
	return acquireFrontierStateStartupLeaseWithSystem(
		path,
		timeout,
		defaultFrontierStateStartupLeaseSystem(),
	)
}

func acquireFrontierStateStartupLeaseWithSystem(
	path string,
	timeout time.Duration,
	system frontierStateStartupLeaseSystem,
) (*frontierStateStartupLease, error) {
	if timeout <= 0 {
		return nil, errors.New("acquire frontier state startup lease: timeout must be positive")
	}
	leasePath := path + frontierStateStartupLeaseSuffix
	file, err := system.open(leasePath, os.O_CREATE|os.O_RDWR, frontierStateFileMode)
	if err != nil {
		return nil, fmt.Errorf("open frontier state startup lease: %w", err)
	}
	if err := system.secure(file, frontierStateFileMode); err != nil {
		return nil, errors.Join(
			fmt.Errorf("secure frontier state startup lease: %w", err),
			system.close(file),
		)
	}
	if err := lockFrontierStateStartupLease(file, timeout, system); err != nil {
		return nil, errors.Join(err, system.close(file))
	}

	return &frontierStateStartupLease{file: file, system: system}, nil
}

func lockFrontierStateStartupLease(
	file *os.File,
	timeout time.Duration,
	system frontierStateStartupLeaseSystem,
) error {
	startedAt := system.now()
	for {
		err := system.lock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("lock frontier state startup lease: %w", err)
		}
		remaining := timeout - system.now().Sub(startedAt)
		if remaining <= 0 {
			return fmt.Errorf(
				"lock frontier state startup lease: %w",
				errFrontierStateStartupLeaseTimeout,
			)
		}
		system.wait(min(remaining, frontierStateStartupLeaseRetry))
	}
}

func (lease *frontierStateStartupLease) release() error {
	unlockErr := lease.system.lock(int(lease.file.Fd()), syscall.LOCK_UN)
	closeErr := lease.system.close(lease.file)
	if err := errors.Join(unlockErr, closeErr); err != nil {
		return fmt.Errorf("release frontier state startup lease: %w", err)
	}

	return nil
}

func runWithFrontierStateStartupLease(
	path string,
	timeout time.Duration,
	operation func() error,
) (err error) {
	lease, err := acquireFrontierStateStartupLease(path, timeout)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lease.release())
	}()

	return operation()
}
