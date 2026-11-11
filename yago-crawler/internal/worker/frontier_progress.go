package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const messageFrontierProgressFailed = "crawler frontier progress report failed"

func (worker *Worker) reportPersistentFrontier(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	runID string,
) {
	worker.enqueueFrontierProgress(ctx, order, runID)
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.enqueueFrontierProgress(ctx, order, runID)
		}
	}
}

func (worker *Worker) enqueueFrontierProgress(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	runID string,
) {
	depth, pendingIngest, err := worker.frontier.ProgressForRun(ctx, runID)
	if err != nil {
		if ctx.Err() == nil {
			slog.WarnContext(ctx, messageFrontierProgressFailed, slog.Any("error", err))
		}
		return
	}
	worker.enqueueProgress(ctx, order, "", runTally(depth, pendingIngest))
}
