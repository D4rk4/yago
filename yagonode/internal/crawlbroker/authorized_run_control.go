package crawlbroker

import (
	"context"
	"fmt"
)

func (r *ControlRegistry) reassignAuthorizedRun(
	ctx context.Context,
	workerID string,
	runID string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	assignedWorkerID, assigned := r.runWorkers[runID]
	if assigned && assignedWorkerID == workerID {
		return nil
	}
	if err := r.directives.ReconcileRun(ctx, workerID, runID, false); err != nil {
		return fmt.Errorf("reassign authorized crawl control run: %w", err)
	}
	r.runWorkers[runID] = workerID

	return nil
}
