package frontier

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

type deferredPageCheckpoint interface {
	DeferPage(context.Context, []byte, string, uint8, time.Time) error
}

func (f *Frontier) Defer(
	ctx context.Context,
	work crawljob.CrawlJob,
	retryAfter time.Time,
) (bool, bool) {
	if work.RetryAttempt == ^uint8(0) || retryAfter.IsZero() {
		return false, false
	}
	run, durable := f.acquireRunDurability(work.RunID)
	if f.abandonStaleLeaseJob(work, run, durable) {
		return true, false
	}
	if durable {
		checkpoint, ok := f.checkpoint.(deferredPageCheckpoint)
		if !ok {
			f.failDeferredPage(work, run, durable, fmt.Errorf(
				"defer crawl page: %w",
				frontiercheckpoint.ErrInvalidPage,
			))
			return true, false
		}
		if err := checkpoint.DeferPage(
			context.WithoutCancel(ctx),
			work.Provenance,
			work.URL,
			work.RetryAttempt+1,
			retryAfter,
		); err != nil {
			f.failDeferredPage(work, run, durable, fmt.Errorf("defer crawl page: %w", err))
			return true, false
		}
	}
	f.mu.Lock()
	if f.state.runs[work.RunID] != run || !runLeaseMatchesJob(run, work) {
		if durable {
			f.finishRunDurabilityLocked(work.RunID, run, nil)
		}
		f.mu.Unlock()
		return true, true
	}
	if work.RetryAttempt == 0 && run.firstPassPending > 0 {
		run.firstPassPending--
	}
	candidate := candidateFromJob(work)
	candidate.retryAttempt = work.RetryAttempt + 1
	candidate.retryAfter = retryAfter
	run.clearPageHostProgress(work)
	run.appendRetry(candidate.host, pendingPageFromCandidate(candidate))
	f.releaseHost(work.URL)
	if durable {
		f.finishRunDurabilityLocked(work.RunID, run, nil)
	}
	f.rebalanceReadyLocked()
	f.mu.Unlock()
	f.wake()
	return true, true
}

func (f *Frontier) failDeferredPage(
	work crawljob.CrawlJob,
	run *crawlRun,
	durable bool,
	err error,
) {
	f.mu.Lock()
	f.releaseHost(work.URL)
	if durable {
		f.finishRunDurabilityLocked(work.RunID, run, err)
	} else {
		f.recordCheckpointFailureLocked(err)
	}
	f.mu.Unlock()
	f.wake()
}
