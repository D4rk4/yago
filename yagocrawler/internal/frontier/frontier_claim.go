package frontier

import (
	"context"
	"time"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
)

type takeResult uint8

const (
	takeWaiting takeResult = iota
	takeClaimed
	takeStopped
	takeClosed
)

func (f *Frontier) claimNext(
	ctx context.Context,
	now time.Time,
) (crawljob.CrawlJob, time.Duration, takeResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ctx.Err() != nil {
		return crawljob.CrawlJob{}, 0, takeStopped
	}
	f.demoteControlBlockedReadyLocked()
	f.refillReadyLocked()
	next, index, wait, due := f.nextDue(now)
	if due {
		f.claimReadyLocked(next, index, now)

		return next, 0, takeClaimed
	}
	if f.hasPendingReadyCandidateLocked() {
		pendingWait := f.rebuildReadyForDispatchLocked(now)
		next, index, rebuiltWait, rebuiltDue := f.nextDue(now)
		wait = earlierWait(wait, earlierWait(pendingWait, rebuiltWait))
		if rebuiltDue {
			f.claimReadyLocked(next, index, now)

			return next, 0, takeClaimed
		}
	}
	if f.closing && len(f.state.ready) == 0 {
		return crawljob.CrawlJob{}, 0, takeClosed
	}

	return crawljob.CrawlJob{}, wait, takeWaiting
}

func (f *Frontier) claimReadyLocked(job crawljob.CrawlJob, index int, now time.Time) {
	f.pace.Visited(job, now)
	f.recordRateVisitLocked(job.Provenance, now)
	f.acquireHost(job.URL)
	f.nextDispatchOrder++
	f.dispatchOrder[job.RunID] = f.nextDispatchOrder
	f.removeReadyAtLocked(index)
	f.refillReadyLocked()
}
