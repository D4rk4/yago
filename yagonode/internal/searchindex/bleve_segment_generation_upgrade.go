package searchindex

import (
	"bytes"
	"errors"
	"fmt"
	"os"
)

var bleveCompatibleSegmentGeneration = []byte("zapx-v17-empty-chunk-offsets-v1\n")

func admitBleveSegmentGeneration(root string, canRebuild bool) error {
	present, err := bleveIndexRootPresent(root)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	value, err := os.ReadFile(bleveSegmentGenerationPath(root))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read bleve segment generation: %w", err)
	}
	if bytes.Equal(value, bleveSegmentGeneration) {
		return nil
	}
	if bytes.Equal(value, bleveCompatibleSegmentGeneration) {
		return persistCurrentBleveSegmentGeneration(root)
	}
	if !canRebuild {
		return fmt.Errorf("unstamped bleve index requires a document rebuild source")
	}

	return requireBleveRebuild(root)
}
