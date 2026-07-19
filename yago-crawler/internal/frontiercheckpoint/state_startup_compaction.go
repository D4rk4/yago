package frontiercheckpoint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

const (
	frontierStateCompactionSuffix = ".compacting"
	frontierStateCompactTxBytes   = int64(16) << 20
	frontierStateFileMode         = os.FileMode(0o600)
)

type frontierStateCompaction struct {
	beforeBytes int64
	afterBytes  int64
	installed   bool
}

type frontierStateCompactionFilesystem struct {
	remove        func(string) error
	inspect       func(string) (os.FileInfo, error)
	copy          func(string, string) error
	replace       func(string, string) error
	syncDirectory func(string) error
}

type StateMaintenanceAdmission interface {
	RunMaintenanceWithHeadroom(
		func() (uint64, error),
		func(uint64) error,
	) error
}

var openFrontierStateDirectory = os.Open

func compactFrontierState(
	path string,
	maximumBytes uint64,
	maintenance StateMaintenanceAdmission,
) (frontierStateCompaction, error) {
	return compactFrontierStateWithFilesystem(
		path,
		maximumBytes,
		maintenance,
		frontierStateCompactionFilesystem{
			remove:        os.Remove,
			inspect:       os.Stat,
			copy:          copyCompactedFrontierState,
			replace:       os.Rename,
			syncDirectory: syncFrontierStateDirectory,
		},
	)
}

func compactFrontierStateWithFilesystem(
	path string,
	maximumBytes uint64,
	maintenance StateMaintenanceAdmission,
	filesystem frontierStateCompactionFilesystem,
) (frontierStateCompaction, error) {
	temporaryPath := path + frontierStateCompactionSuffix
	if err := filesystem.remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return frontierStateCompaction{}, fmt.Errorf(
			"clear stale frontier compaction file: %w",
			err,
		)
	}
	if maximumBytes == 0 {
		return frontierStateCompaction{}, nil
	}
	info, err := filesystem.inspect(path)
	if errors.Is(err, os.ErrNotExist) {
		return frontierStateCompaction{}, nil
	}
	if err != nil {
		return frontierStateCompaction{}, fmt.Errorf(
			"inspect frontier state for compaction: %w",
			err,
		)
	}
	result := frontierStateCompaction{beforeBytes: info.Size()}
	if !frontierStateMaximumReached(info.Size(), maximumBytes) {
		return result, nil
	}
	if err := copyFrontierStateWithAdmission(
		path,
		temporaryPath,
		maintenance,
		filesystem,
	); err != nil {
		_ = filesystem.remove(temporaryPath)

		return result, err
	}
	if err := filesystem.replace(temporaryPath, path); err != nil {
		_ = filesystem.remove(temporaryPath)

		return result, fmt.Errorf("install compacted frontier state: %w", err)
	}
	result.installed = true
	if err := filesystem.syncDirectory(filepath.Dir(path)); err != nil {
		return result, err
	}
	after, err := filesystem.inspect(path)
	if err != nil {
		return result, fmt.Errorf("inspect compacted frontier state: %w", err)
	}
	result.afterBytes = after.Size()

	return result, nil
}

func copyFrontierStateWithAdmission(
	path string,
	temporaryPath string,
	maintenance StateMaintenanceAdmission,
	filesystem frontierStateCompactionFilesystem,
) error {
	if maintenance == nil {
		return errors.New(
			"copy compacted frontier state: storage maintenance admission unavailable",
		)
	}
	if err := maintenance.RunMaintenanceWithHeadroom(
		func() (uint64, error) {
			info, err := filesystem.inspect(path)
			if err != nil {
				return 0, fmt.Errorf("measure frontier state compaction headroom: %w", err)
			}
			size := info.Size()
			if size < 0 {
				return 0, errors.New(
					"measure frontier state compaction headroom: invalid source size",
				)
			}

			return uint64(size), nil
		},
		func(uint64) error {
			return filesystem.copy(path, temporaryPath)
		},
	); err != nil {
		return fmt.Errorf("admit frontier state compaction: %w", err)
	}

	return nil
}

func copyCompactedFrontierState(path, temporaryPath string) error {
	source, err := bolt.Open(path, frontierStateFileMode, &bolt.Options{
		ReadOnly: true,
		Timeout:  databaseLockTimeout,
	})
	if err != nil {
		return fmt.Errorf("open frontier state for compaction: %w", err)
	}
	destination, err := bolt.Open(
		temporaryPath,
		frontierStateFileMode,
		&bolt.Options{Timeout: databaseLockTimeout},
	)
	if err != nil {
		_ = source.Close()

		return fmt.Errorf("open frontier compaction target: %w", err)
	}

	return finishFrontierStateCopy(
		destination,
		source,
		temporaryPath,
		bolt.Compact,
		os.Chmod,
	)
}

func finishFrontierStateCopy(
	destination *bolt.DB,
	source *bolt.DB,
	temporaryPath string,
	compact func(*bolt.DB, *bolt.DB, int64) error,
	secure func(string, os.FileMode) error,
) error {
	compactErr := compact(destination, source, frontierStateCompactTxBytes)
	modeErr := secure(temporaryPath, frontierStateFileMode)
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
		return fmt.Errorf("copy compacted frontier state: %w", err)
	}

	return nil
}

type frontierStateDirectory interface {
	Sync() error
	Close() error
}

func syncFrontierStateDirectory(path string) error {
	directory, err := openFrontierStateDirectory(path)
	if err != nil {
		return fmt.Errorf("open frontier state directory: %w", err)
	}

	return finishFrontierStateDirectorySync(directory)
}

func finishFrontierStateDirectorySync(directory frontierStateDirectory) error {
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("sync frontier state directory: %w", err)
	}

	return nil
}
