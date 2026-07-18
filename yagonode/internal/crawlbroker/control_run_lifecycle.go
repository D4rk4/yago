package crawlbroker

import (
	"context"
	"fmt"
)

func (r *ControlRegistry) ReassignRunIfLeaseOwned(
	ctx context.Context,
	queue *DurableOrderQueue,
	workerID string,
	runID string,
) (runLeaseOwnership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ledger, ok := r.directives.(*persistentControlDirectiveLedger); ok {
		return ledger.ReassignRunIfLeaseOwned(ctx, queue, workerID, runID)
	}
	ownership, err := queue.runLeaseOwnership(ctx, workerID, runID)
	if err != nil || ownership != runLeaseOwnedByWorker {
		return ownership, err
	}

	if err := r.directives.ReconcileRun(ctx, workerID, runID, false); err != nil {
		return ownership, fmt.Errorf("reassign crawl control run: %w", err)
	}

	return ownership, nil
}

func (r *ControlRegistry) CompleteRun(
	ctx context.Context,
	target leaseControlTarget,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.directives.ReconcileRun(ctx, target.WorkerID, target.RunID, true); err != nil {
		return fmt.Errorf("complete crawl control run: %w", err)
	}

	return nil
}
