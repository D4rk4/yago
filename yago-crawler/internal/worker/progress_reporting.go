package worker

import (
	"context"
	"encoding/hex"
	"log/slog"
	"sort"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type progressUpdate struct {
	order   yagocrawlcontract.CrawlOrder
	leaseID string
	tally   yagocrawlcontract.CrawlRunTally
}

func (worker *Worker) enqueueProgress(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	leaseID string,
	tally yagocrawlcontract.CrawlRunTally,
) {
	runID := hex.EncodeToString(order.Provenance)
	if runID == "" || ctx.Err() != nil {
		return
	}
	worker.progressMutex.Lock()
	if worker.pendingProgress == nil {
		worker.pendingProgress = make(map[string]progressUpdate)
	}
	worker.pendingProgress[runID] = progressUpdate{
		order:   order,
		leaseID: leaseID,
		tally:   tally,
	}
	worker.progressMutex.Unlock()
	select {
	case worker.progressWake <- struct{}{}:
	default:
	}
}

func (worker *Worker) runProgressReports(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-worker.progressWake:
			for _, update := range worker.takePendingProgress() {
				if ctx.Err() != nil {
					return
				}
				if err := worker.exchange.ReportProgress(
					ctx,
					update.order,
					update.leaseID,
					update.tally,
				); err != nil {
					slog.WarnContext(ctx, messageHeartbeat, slog.Any("error", err))
				}
			}
		}
	}
}

func (worker *Worker) takePendingProgress() []progressUpdate {
	worker.progressMutex.Lock()
	updates := make([]progressUpdate, 0, len(worker.pendingProgress))
	for _, update := range worker.pendingProgress {
		updates = append(updates, update)
	}
	clear(worker.pendingProgress)
	worker.progressMutex.Unlock()
	sort.Slice(updates, func(left, right int) bool {
		return hex.EncodeToString(updates[left].order.Provenance) <
			hex.EncodeToString(updates[right].order.Provenance)
	})
	return updates
}
