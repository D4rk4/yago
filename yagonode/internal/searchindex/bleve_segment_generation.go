package searchindex

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const bleveSegmentGenerationSuffix = ".segment-generation"

var (
	bleveSegmentGeneration      = []byte("bleve-v2.6.1-zapx-v17.2.3-v2\n")
	writeBleveSegmentGeneration = os.WriteFile
)

func bleveSegmentGenerationPath(root string) string {
	return filepath.Clean(root) + bleveSegmentGenerationSuffix
}

func bleveSegmentGenerationIsCurrent(root string) (bool, error) {
	value, err := os.ReadFile(bleveSegmentGenerationPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read bleve segment generation: %w", err)
	}

	return bytes.Equal(value, bleveSegmentGeneration), nil
}

func requireCurrentBleveSegmentGeneration(root string, canRebuild bool) error {
	return admitBleveSegmentGeneration(root, canRebuild)
}

func bleveIndexRootPresent(root string) (bool, error) {
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect bleve index root: %w", err)
	}
	if !info.IsDir() {
		return true, nil
	}
	if _, err := os.Stat(filepath.Join(root, "index_meta.json")); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect bleve index metadata: %w", err)
	}
	for shard := range diskShardCount {
		_, err := os.Stat(diskShardPath(root, shard))
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect bleve index shard: %w", err)
		}
	}

	return false, nil
}

func persistCurrentBleveSegmentGeneration(root string) error {
	if err := writeBleveSegmentGeneration(
		bleveSegmentGenerationPath(root),
		bleveSegmentGeneration,
		0o600,
	); err != nil {
		return fmt.Errorf("persist bleve segment generation: %w", err)
	}

	return nil
}
