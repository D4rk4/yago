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
	if err := r.directives.ReconcileRun(ctx, workerID, runID, false); err != nil {
		return fmt.Errorf("reassign authorized crawl control run: %w", err)
	}

	return nil
}
