package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

const messageHostRateLimited = "crawler host temporarily deferred after rate limiting"

type pageExecutionOutcome struct {
	host string
	err  error
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
			slog.String("pageId", page.ID),
			slog.String("host", page.Host),
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
	if result.rateLimited {
		cooldownUntil, err := worker.frontier.RecordHostRateLimit(
			ctx,
			page.Host,
			time.Now(),
			result.retryAt,
		)
		if err != nil {
			return err
		}
		result.retryAt = cooldownUntil
		slog.WarnContext(ctx, messageHostRateLimited,
			slog.String("host", page.Host),
			slog.Duration("cooldown", time.Until(cooldownUntil)))
	}
	if err := worker.frontier.SettleWithOutcome(
		ctx,
		page.ID,
		result.state,
		result.retryAt,
		result.outcome,
	); err != nil {
		return err
	}
	return nil
}
