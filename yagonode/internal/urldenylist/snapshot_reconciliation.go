package urldenylist

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const snapshotReconciliationBudget = time.Second

func (s *Store) reconcileFailedMutation(
	ctx context.Context,
	mutationError error,
	kind Kind,
	value string,
	adding bool,
) error {
	reconciliationContext, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		snapshotReconciliationBudget,
	)
	defer cancel()
	snapshot, err := s.loadSnapshot(reconciliationContext)
	if err == nil {
		s.snapshots.current.Store(&snapshot)

		return mutationError
	}
	if adding {
		s.snapshots.storeAdded(kind, value)
	}

	return errors.Join(
		mutationError,
		fmt.Errorf("reconcile denylist snapshot: %w", err),
	)
}
