package searchindex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const bleveRebuildStateSuffix = ".rebuild-required"

var (
	statBleveRebuildState   = os.Stat
	writeBleveRebuildState  = os.WriteFile
	removeBleveRebuildState = os.Remove
)

type bleveRebuildRequirement struct {
	root    string
	pending bool
}

func bleveRebuildStatePath(root string) string {
	return filepath.Clean(root) + bleveRebuildStateSuffix
}

func bleveRebuildPending(root string) (bool, error) {
	_, err := statBleveRebuildState(bleveRebuildStatePath(root))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	return false, fmt.Errorf("inspect bleve rebuild state: %w", err)
}

func prepareBleveRebuildRequirement(
	root string,
	canRebuild bool,
) (*bleveRebuildRequirement, error) {
	pending, err := bleveRebuildPending(root)
	if err != nil {
		return nil, err
	}
	requirement := &bleveRebuildRequirement{root: root, pending: pending}
	if !pending {
		return requirement, nil
	}
	if !canRebuild {
		return nil, fmt.Errorf("bleve index rebuild requires documents")
	}
	if err := removeBleveDisk(root); err != nil {
		return nil, fmt.Errorf("restart bleve index rebuild: %w", err)
	}

	return requirement, nil
}

func (r *bleveRebuildRequirement) require() error {
	if r.pending {
		return nil
	}
	if err := requireBleveRebuild(r.root); err != nil {
		return err
	}
	r.pending = true

	return nil
}

func requireBleveRebuild(root string) error {
	if err := os.MkdirAll(filepath.Dir(bleveRebuildStatePath(root)), 0o750); err != nil {
		return fmt.Errorf("create bleve rebuild state directory: %w", err)
	}
	if err := writeBleveRebuildState(
		bleveRebuildStatePath(root),
		[]byte("required\n"),
		0o600,
	); err != nil {
		return fmt.Errorf("persist bleve rebuild state: %w", err)
	}

	return nil
}

func completeBleveRebuild(root string) error {
	if err := removeBleveRebuildState(bleveRebuildStatePath(root)); err != nil {
		return fmt.Errorf("clear bleve rebuild state: %w", err)
	}

	return nil
}
