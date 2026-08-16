package searchindex

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const bleveSegmentGenerationSuffix = ".segment-generation"

var bleveSegmentGeneration = []byte("zapx-v17-empty-chunk-offsets-v1\n")

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
	present, err := bleveIndexRootPresent(root)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	current, err := bleveSegmentGenerationIsCurrent(root)
	if err != nil {
		return err
	}
	if current {
		return nil
	}
	if !canRebuild {
		return fmt.Errorf("unstamped bleve index requires a document rebuild source")
	}

	return requireBleveRebuild(root)
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
	if err := os.WriteFile(
		bleveSegmentGenerationPath(root),
		bleveSegmentGeneration,
		0o600,
	); err != nil {
		return fmt.Errorf("persist bleve segment generation: %w", err)
	}

	return nil
}
