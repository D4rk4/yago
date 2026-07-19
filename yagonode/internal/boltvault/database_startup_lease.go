package boltvault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	databaseStartupLeaseSuffix        = ".startup.lock"
	databaseStartupLeaseRetryInterval = 10 * time.Millisecond
)

var (
	createDatabaseStartupLeaseDirectory = os.MkdirAll
	openDatabaseStartupLeaseFile        = os.OpenFile
	secureDatabaseStartupLeaseFile      = os.Chmod
	tryDatabaseStartupLease             = func(file *os.File) error {
		return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	releaseDatabaseStartupLease = func(file *os.File) error {
		return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}
	databaseStartupLeaseNow  = time.Now
	databaseStartupLeaseWait = time.Sleep
)

type DatabaseStartupLease struct {
	file *os.File
}

func AcquireDatabaseStartupLease(
	databasePath string,
	timeout time.Duration,
) (*DatabaseStartupLease, error) {
	if strings.TrimSpace(databasePath) == "" {
		return nil, errors.New("acquire database startup lease: database path is required")
	}
	if timeout <= 0 {
		return nil, errors.New("acquire database startup lease: timeout must be positive")
	}
	if err := createDatabaseStartupLeaseDirectory(
		filepath.Dir(databasePath),
		0o750,
	); err != nil {
		return nil, fmt.Errorf("create database startup lease directory: %w", err)
	}
	leasePath := filepath.Clean(databasePath) + databaseStartupLeaseSuffix
	file, err := openDatabaseStartupLeaseFile(
		leasePath,
		os.O_CREATE|os.O_RDWR,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open database startup lease: %w", err)
	}
	if err := secureDatabaseStartupLeaseFile(leasePath, 0o600); err != nil {
		return nil, finishDatabaseStartupLeaseAttempt(
			file,
			fmt.Errorf("secure database startup lease: %w", err),
		)
	}

	deadline := databaseStartupLeaseNow().Add(timeout)
	for {
		err = tryDatabaseStartupLease(file)
		if err == nil {
			return &DatabaseStartupLease{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, finishDatabaseStartupLeaseAttempt(
				file,
				fmt.Errorf("lock database startup lease: %w", err),
			)
		}
		remaining := deadline.Sub(databaseStartupLeaseNow())
		if remaining <= 0 {
			return nil, finishDatabaseStartupLeaseAttempt(
				file,
				fmt.Errorf("wait for database startup lease: %w", os.ErrDeadlineExceeded),
			)
		}
		databaseStartupLeaseWait(min(remaining, databaseStartupLeaseRetryInterval))
	}
}

func finishDatabaseStartupLeaseAttempt(file *os.File, failure error) error {
	return errors.Join(failure, file.Close())
}

func (lease *DatabaseStartupLease) Release() error {
	if err := errors.Join(
		releaseDatabaseStartupLease(lease.file),
		lease.file.Close(),
	); err != nil {
		return fmt.Errorf("release database startup lease: %w", err)
	}

	return nil
}
