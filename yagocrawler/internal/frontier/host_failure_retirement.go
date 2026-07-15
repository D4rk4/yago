package frontier

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

const (
	msgHostFailureBudgetExhausted = "crawl host failure budget exhausted"
	hostFailureRetirementMinimum  = 5
)

func (f *Frontier) RecordHostFetchOutcome(
	ctx context.Context,
	work crawljob.CrawlJob,
	failed bool,
) {
	host := weburl.Host(work.URL)
	if host == "" {
		return
	}

	f.mu.Lock()
	run := f.state.runs[work.RunID]
	if run == nil {
		f.mu.Unlock()

		return
	}
	if _, retired := run.retiredHosts[host]; retired {
		f.mu.Unlock()

		return
	}
	if !failed {
		delete(run.hostFailures, host)
		f.mu.Unlock()

		return
	}
	run.hostFailures[host]++
	if run.hostFailures[host] < hostFailureRetirementMinimum {
		f.mu.Unlock()

		return
	}
	delete(run.hostFailures, host)
	run.retiredHosts[host] = struct{}{}
	dropped, finish := f.retireHostQueuedLocked(work.RunID, host)
	f.mu.Unlock()

	slog.WarnContext(ctx, msgHostFailureBudgetExhausted,
		slog.String("runId", work.RunID.String()),
		slog.String("host", host),
		slog.Int("consecutiveFailures", hostFailureRetirementMinimum),
		slog.Int("dropped", dropped),
	)
	if finish != nil {
		f.scheduleSettlement(finish.finish, finish.succeeded)
	}
	f.wake()
}

func (f *Frontier) retireHostQueuedLocked(runID uuid.UUID, host string) (int, *runFinish) {
	settled := f.retireReadyHostLocked(runID, host)
	if run := f.state.runs[runID]; run != nil {
		settled += run.clearPendingHost(host)
	}
	f.refillReadyLocked()
	if settled == 0 {
		return 0, nil
	}

	return settled, f.settleQueuedManyLocked(runID, settled)
}

func (f *Frontier) retireReadyHostLocked(runID uuid.UUID, host string) int {
	settled := 0
	ready := f.state.ready
	kept := ready[:0]
	clear(f.readyPerRun)
	for _, job := range ready {
		if job.RunID == runID && weburl.Host(job.URL) == host {
			settled++

			continue
		}
		kept = append(kept, job)
		f.readyPerRun[job.RunID]++
	}
	clear(ready[len(kept):])
	f.state.ready = kept

	return settled
}
