package crawlbroker

import (
	"context"
	"fmt"
)

func (r *ControlRegistry) CompleteRun(
	ctx context.Context,
	target leaseControlTarget,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.directives.ReconcileRun(ctx, target.WorkerID, target.RunID, true); err != nil {
		return fmt.Errorf("complete crawl control run: %w", err)
	}
	delete(r.runWorkers, target.RunID)

	return nil
}
