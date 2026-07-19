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
	root      string
	pending   bool
	preflight func() error
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
	preflights ...func() error,
) (*bleveRebuildRequirement, error) {
	pending, err := bleveRebuildPending(root)
	if err != nil {
		return nil, err
	}
	var preflight func() error
	if len(preflights) > 0 {
		preflight = preflights[0]
	}
	requirement := &bleveRebuildRequirement{
		root: root, pending: pending, preflight: preflight,
	}
	if !pending {
		return requirement, nil
	}
	if !canRebuild {
		return nil, fmt.Errorf("bleve index rebuild requires documents")
	}

	return requirement, nil
}

func (r *bleveRebuildRequirement) prepare() error {
	if r.preflight != nil {
		if err := r.preflight(); err != nil {
			return err
		}
	}

	return nil
}

func (r *bleveRebuildRequirement) require() error {
	if r.pending {
		return nil
	}
	if err := requireBleveRebuild(r.root); err != nil {
		return err
	}
	r.pending = true

	return r.prepare()
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
