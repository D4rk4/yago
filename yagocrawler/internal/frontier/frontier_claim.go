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
	selection := f.nextDue(now)
	if selection.due {
		f.claimReadyLocked(selection, now)

		return selection.job, 0, takeClaimed
	}
	if f.hasPendingReadyCandidateLocked() {
		pendingWait := f.rebuildReadyForDispatchLocked(now)
		rebuilt := f.nextDue(now)
		selection.wait = earlierWait(selection.wait, earlierWait(pendingWait, rebuilt.wait))
		if rebuilt.due {
			f.claimReadyLocked(rebuilt, now)

			return rebuilt.job, 0, takeClaimed
		}
	}
	if f.closing && len(f.state.ready) == 0 {
		return crawljob.CrawlJob{}, 0, takeClosed
	}

	return crawljob.CrawlJob{}, selection.wait, takeWaiting
}

func (f *Frontier) claimReadyLocked(selection readySelection, now time.Time) {
	f.pace.Visited(selection.job, now)
	f.recordRateVisitLocked(selection.job.Provenance, now)
	f.acquireHost(selection.job.URL)
	f.nextDispatchOrder++
	f.dispatchOrder[selection.job.RunID] = f.nextDispatchOrder
	f.recordAutomaticDiscoveryDispatchLocked(selection.job, selection.contended)
	f.removeReadyAtLocked(selection.index)
	f.refillReadyLocked()
}
