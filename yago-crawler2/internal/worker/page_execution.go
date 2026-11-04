package worker

import (
	"context"
	"errors"
	"log/slog"

	"github.com/D4rk4/yago/yago-crawler2/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type pageExecutionOutcome struct {
	err error
}

func (worker *Worker) executeClaimedPage(
	ctx context.Context,
	order yagocrawlcontract.CrawlOrder,
	rules runRules,
	runID string,
	leaseID string,
	page frontier.Page,
) error {
	pageContext, stopPage := context.WithTimeout(ctx, maximumPageAttemptDuration)
	result, pageErr := worker.processPage(pageContext, order, rules, runID, page)
	pageExpired := errors.Is(pageContext.Err(), context.DeadlineExceeded)
	stopPage()
	if pageErr != nil {
		return pageErr
	}
	if pageExpired {
		slog.WarnContext(ctx, messagePageAttemptExpired,
			slog.String("url", page.URL),
			slog.String("stage", result.stage),
			slog.Duration("deadline", maximumPageAttemptDuration))
	}
	if len(result.pendingIngestJSON) > 0 {
		if err := worker.frontier.StorePendingIngest(
			ctx,
			page.ID,
			leaseID,
			result.pendingIngestJSON,
		); err != nil {
			return err
		}
	}
	if err := worker.frontier.Settle(ctx, page.ID, result.state, result.retryAt); err != nil {
		return err
	}
	depth, err := worker.frontier.DepthForRun(ctx, runID)
	if err != nil {
		return err
	}
	worker.enqueueProgress(ctx, order, leaseID, runTally(depth))
	return nil
}
