package boltvault

import (
	"errors"
	"fmt"
	"os"
	"time"

	bolt "go.etcd.io/bbolt"
)

const (
	compactedFileTransactionBytes = int64(16) << 20
	compactedFileMode             = os.FileMode(0o600)
)

func CopyCompactedFile(sourcePath, destinationPath string, lockTimeout time.Duration) error {
	if lockTimeout <= 0 {
		return errors.New("copy compacted file: lock timeout must be positive")
	}
	source, err := bolt.Open(sourcePath, compactedFileMode, &bolt.Options{
		ReadOnly: true,
		Timeout:  lockTimeout,
	})
	if err != nil {
		return fmt.Errorf("open compacted-file source: %w", err)
	}
	destination, err := bolt.Open(
		destinationPath,
		compactedFileMode,
		&bolt.Options{Timeout: lockTimeout},
	)
	if err != nil {
		_ = source.Close()

		return fmt.Errorf("open compacted-file destination: %w", err)
	}

	return finishCompactedFileCopy(destination, source, destinationPath, bolt.Compact, os.Chmod)
}

func finishCompactedFileCopy(
	destination *bolt.DB,
	source *bolt.DB,
	destinationPath string,
	compact func(*bolt.DB, *bolt.DB, int64) error,
	secure func(string, os.FileMode) error,
) error {
	compactErr := compact(destination, source, compactedFileTransactionBytes)
	modeErr := secure(destinationPath, compactedFileMode)
	syncErr := destination.Sync()
	destinationCloseErr := destination.Close()
	sourceCloseErr := source.Close()
	if err := errors.Join(
		compactErr,
		modeErr,
		syncErr,
		destinationCloseErr,
		sourceCloseErr,
	); err != nil {
		return fmt.Errorf("copy compacted file: %w", err)
	}

	return nil
}
