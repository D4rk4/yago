package frontier

import "github.com/D4rk4/yago/yago-crawler/internal/crawljob"

func (f *Frontier) SuspendLostLeaseRun(work crawljob.CrawlJob) bool {
	f.mu.Lock()
	run := f.state.runs[work.RunID]
	if !runLeaseMatchesJob(run, work) {
		f.abandonJobLocked(work, run)
		f.mu.Unlock()
		f.wake()

		return false
	}
	f.abandonJobLocked(work, run)
	provenance := run.provenance
	f.suspended[provenance] = struct{}{}
	finishes := f.cancelQueuedLocked(provenance)
	f.mu.Unlock()

	f.scheduleSettlements(finishes)
	f.wake()

	return true
}
