package yagonode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
)

const crawlStateCompactionSuffix = ".compacting"

type crawlStateCompaction struct {
	beforeBytes int64
	afterBytes  int64
	installed   bool
}

type crawlStateCompactionFilesystem struct {
	remove        func(string) error
	inspect       func(string) (os.FileInfo, error)
	copy          func(string, string) error
	replace       func(string, string) error
	syncDirectory func(string) error
}

var openCrawlStateDirectory = os.Open

func compactCrawlRuntimeState(
	path string,
	maximumBytes int64,
	admissions ...growthAdmission,
) (crawlStateCompaction, error) {
	var admission growthAdmission
	if len(admissions) > 0 {
		admission = admissions[0]
	}

	return compactCrawlRuntimeStateWithFilesystem(
		path,
		maximumBytes,
		crawlStateCompactionFilesystem{
			remove:        os.Remove,
			inspect:       os.Stat,
			copy:          copyCompactedCrawlRuntimeState,
			replace:       os.Rename,
			syncDirectory: syncCrawlRuntimeStateDirectory,
		},
		admission,
	)
}

func compactCrawlRuntimeStateWithFilesystem(
	path string,
	maximumBytes int64,
	filesystem crawlStateCompactionFilesystem,
	admissions ...growthAdmission,
) (crawlStateCompaction, error) {
	temporaryPath := path + crawlStateCompactionSuffix
	if err := filesystem.remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return crawlStateCompaction{}, fmt.Errorf(
			"clear stale crawl state compaction file: %w",
			err,
		)
	}
	if maximumBytes == 0 {
		return crawlStateCompaction{}, nil
	}
	info, err := filesystem.inspect(path)
	if errors.Is(err, os.ErrNotExist) {
		return crawlStateCompaction{}, nil
	}
	if err != nil {
		return crawlStateCompaction{}, fmt.Errorf("inspect crawl state for compaction: %w", err)
	}
	result := crawlStateCompaction{beforeBytes: info.Size()}
	if info.Size() < maximumBytes {
		return result, nil
	}
	var admission growthAdmission
	if len(admissions) > 0 {
		admission = admissions[0]
	}
	_, err = runStorageMaintenance(
		admission,
		func() (uint64, error) {
			measured, measureErr := filesystem.inspect(path)
			if measureErr != nil {
				return 0, fmt.Errorf("measure crawl state compaction source: %w", measureErr)
			}
			result.beforeBytes = measured.Size()

			return uint64(max(result.beforeBytes, 0)), nil
		},
		func(uint64) error {
			return installCompactedCrawlRuntimeState(
				path,
				temporaryPath,
				maximumBytes,
				filesystem,
				&result,
			)
		},
	)

	return result, err
}

func installCompactedCrawlRuntimeState(
	path string,
	temporaryPath string,
	maximumBytes int64,
	filesystem crawlStateCompactionFilesystem,
	result *crawlStateCompaction,
) error {
	if result.beforeBytes < maximumBytes {
		return nil
	}
	if err := filesystem.copy(path, temporaryPath); err != nil {
		_ = filesystem.remove(temporaryPath)

		return err
	}
	if err := filesystem.replace(temporaryPath, path); err != nil {
		_ = filesystem.remove(temporaryPath)

		return fmt.Errorf("install compacted crawl state: %w", err)
	}
	result.installed = true
	if err := filesystem.syncDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	after, err := filesystem.inspect(path)
	if err != nil {
		return fmt.Errorf("inspect compacted crawl state: %w", err)
	}
	result.afterBytes = after.Size()

	return nil
}

func copyCompactedCrawlRuntimeState(path, temporaryPath string) error {
	if err := boltvault.CopyCompactedFile(
		path,
		temporaryPath,
		crawlRuntimeStateOpenTimeout,
	); err != nil {
		return fmt.Errorf("copy compacted crawl state: %w", err)
	}

	return nil
}

type crawlStateDirectory interface {
	Sync() error
	Close() error
}

func syncCrawlRuntimeStateDirectory(path string) error {
	directory, err := openCrawlStateDirectory(path)
	if err != nil {
		return fmt.Errorf("open crawl state directory: %w", err)
	}

	return finishCrawlRuntimeStateDirectorySync(directory)
}

func finishCrawlRuntimeStateDirectorySync(directory crawlStateDirectory) error {
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return fmt.Errorf("sync crawl state directory: %w", err)
	}

	return nil
}
