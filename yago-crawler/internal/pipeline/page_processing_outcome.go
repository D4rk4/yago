package pipeline

import (
	"context"
	"errors"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawllease"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type pageProcessingOutcome struct {
	runTally   yagocrawlcontract.CrawlRunTally
	reason     string
	httpStatus uint32
	abandoned  bool
	retryAfter time.Time
}

func (p *Pipeline) finishPage(
	ctx context.Context,
	job crawljob.CrawlJob,
	outcome *pageProcessingOutcome,
) {
	if outcome.abandoned {
		p.frontier.Abandon(job)

		return
	}
	if p.deferPageRetry(ctx, job, outcome.retryAfter) {
		return
	}
	completePage(p.frontier, job, outcome.runTally, outcome.httpStatus, outcome.reason)
}

func (p *Pipeline) deferPageRetry(
	ctx context.Context,
	job crawljob.CrawlJob,
	retryAfter time.Time,
) bool {
	if retryAfter.IsZero() {
		return false
	}
	retrying, ok := p.frontier.(pageRetryFrontier)
	if !ok {
		return false
	}
	consumed, scheduled := retrying.Defer(ctx, job, retryAfter)
	if !consumed {
		return false
	}
	if scheduled {
		if observer, ok := p.observer.(pageRetryObserver); ok {
			observer.PageRetryScheduled()
		}
	}
	return true
}

func (p *Pipeline) finishFailedFetch(
	ctx context.Context,
	job crawljob.CrawlJob,
	fetchErr error,
	outcome *pageProcessingOutcome,
) error {
	outcome.retryAfter, _ = retryableFetchFailure(ctx, fetchErr, job.RetryAttempt, time.Now())
	outcome.httpStatus = pageOutcomeHTTPStatus(fetchErr)
	outcome.reason = fetchOutcomeReason(fetchErr)
	removalErr := p.emitRemovalIfGone(ctx, job, fetchErr)
	outcome.abandoned = ctx.Err() != nil || errors.Is(removalErr, crawllease.ErrLeaseLost)
	if removalErr != nil {
		outcome.reason = pageProcessingFailureReason(removalErr)
		return errors.Join(fetchErr, removalErr)
	}
	return fetchErr
}
