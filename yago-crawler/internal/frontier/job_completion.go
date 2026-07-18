package frontier

import (
	"context"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (f *Frontier) Done(
	work crawljob.CrawlJob,
	outcome yagocrawlcontract.CrawlRunTally,
) {
	run, durable := f.acquireRunDurability(work.RunID)
	if f.abandonStaleLeaseJob(work, run, durable) {
		return
	}
	completion := frontiercheckpoint.PageCompletion{Tally: outcome}
	if durable {
		f.mu.Lock()
		if f.state.runs[work.RunID] == run {
			completion.HostProgress = run.checkpointPageHostProgress(work)
		}
		f.mu.Unlock()
	}
	var checkpointErr error
	if durable {
		checkpointErr = f.checkpoint.CompletePage(
			context.Background(),
			work.Provenance,
			work.URL,
			completion,
		)
	}
	f.mu.Lock()
	if durable && checkpointErr == nil && f.state.runs[work.RunID] == run {
		run.clearPageHostProgress(work)
		run.releaseBoundedResidentPage(work.URL)
	}
	f.state.tally.Commit(work.Provenance, outcome)
	f.releaseHost(work.URL)
	if durable {
		f.finishRunDurabilityLocked(work.RunID, run, checkpointErr)
	}
	finish, succeeded, drained := f.state.completion.Settle(work.RunID)
	if drained {
		f.cleanupRunLocked(work.RunID)
	}
	f.mu.Unlock()
	f.wake()
	if drained && finish != nil {
		f.scheduleSettlement(finish, succeeded)
	}
}
