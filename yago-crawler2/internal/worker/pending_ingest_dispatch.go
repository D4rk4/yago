package worker

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yago-crawler2/internal/frontier"
)

func (worker *Worker) claimPendingIngest(ctx context.Context) (frontier.PendingIngest, bool, error) {
	worker.pendingIngestMutex.Lock()
	defer worker.pendingIngestMutex.Unlock()
	if worker.pendingIngestActive == nil {
		worker.pendingIngestActive = make(map[string]struct{})
	}
	pending, found, err := worker.frontier.NextPendingIngestSkipping(
		ctx,
		time.Now(),
		worker.pendingIngestActive,
	)
	if err != nil || !found {
		return frontier.PendingIngest{}, false, err
	}
	worker.pendingIngestActive[pending.PageID] = struct{}{}
	return pending, true, nil
}

func (worker *Worker) releasePendingIngest(pageID string) {
	worker.pendingIngestMutex.Lock()
	delete(worker.pendingIngestActive, pageID)
	worker.pendingIngestMutex.Unlock()
}
