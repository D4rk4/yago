package frontier

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
)

const (
	msgHostFailureBudgetExhausted = "crawl host failure budget exhausted"
	hostFailureRetirementMinimum  = 5
)

type hostFetchOutcome struct {
	work         crawljob.CrawlJob
	host         string
	failed       bool
	durable      bool
	pace         crawlpace.HostState
	paceCapacity int
}

type hostOutcomePersistence struct {
	progress    frontiercheckpoint.HostProgress
	droppedURLs []string
	persist     bool
	retire      bool
}

func (f *Frontier) RecordHostFetchOutcome(
	ctx context.Context,
	work crawljob.CrawlJob,
	failed bool,
) {
	host := weburl.Host(work.URL)
	if host == "" {
		return
	}
	run, durable := f.acquireRunDurability(work.RunID)
	pace, paceCapacity := f.checkpointHostPace(work.URL)
	outcome := hostFetchOutcome{
		work:         work,
		host:         host,
		failed:       failed,
		durable:      durable,
		pace:         pace,
		paceCapacity: paceCapacity,
	}
	f.mu.Lock()
	if f.skipHostFetchOutcomeLocked(run, outcome) {
		if durable {
			f.finishRunDurabilityLocked(work.RunID, run, nil)
		}
		f.mu.Unlock()
		f.wake()

		return
	}
	persistence, ok := f.applyHostFetchOutcomeLocked(run, outcome)
	if !ok {
		f.mu.Unlock()
		f.wake()

		return
	}
	if durable && persistence.persist {
		run.stagePageHostProgress(
			work,
			host,
			persistence.progress,
			persistence.droppedURLs,
		)
	}
	var dropped int
	var finish *runFinish
	if persistence.retire && f.state.runs[work.RunID] == run {
		dropped, finish = f.retireHostQueuedLocked(work.RunID, host)
	}
	if durable {
		f.finishRunDurabilityLocked(work.RunID, run, nil)
	}
	f.mu.Unlock()
	if !persistence.retire {
		f.wake()

		return
	}

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

func (f *Frontier) skipHostFetchOutcomeLocked(
	run *crawlRun,
	outcome hostFetchOutcome,
) bool {
	if run == nil || f.state.runs[outcome.work.RunID] != run ||
		!runLeaseMatchesJob(run, outcome.work) {
		return true
	}
	if _, retired := run.retiredHosts[outcome.host]; !retired {
		return false
	}
	if outcome.durable && outcome.paceCapacity > 0 {
		run.stagePageHostProgress(
			outcome.work,
			outcome.host,
			frontiercheckpoint.HostProgress{
				Generation:   run.hostOutcomeGeneration(outcome.host),
				Retired:      true,
				Pace:         outcome.pace,
				PaceCapacity: outcome.paceCapacity,
			},
			nil,
		)
	}

	return true
}

func (f *Frontier) applyHostFetchOutcomeLocked(
	run *crawlRun,
	outcome hostFetchOutcome,
) (hostOutcomePersistence, bool) {
	persistence := hostOutcomePersistence{
		progress: frontiercheckpoint.HostProgress{
			Generation:   run.hostOutcomeGeneration(outcome.host),
			Pace:         outcome.pace,
			PaceCapacity: outcome.paceCapacity,
		},
		persist: outcome.paceCapacity > 0,
	}
	if !outcome.failed {
		if run.hostFailures[outcome.host] > 0 {
			persistence.persist = true
			generation, ok := f.advanceHostOutcomeLocked(
				outcome.work.RunID,
				run,
				outcome.host,
				outcome.durable,
			)
			if !ok {
				return hostOutcomePersistence{}, false
			}
			persistence.progress.Generation = generation
		}
		delete(run.hostFailures, outcome.host)

		return persistence, true
	}
	generation, ok := f.advanceHostOutcomeLocked(
		outcome.work.RunID,
		run,
		outcome.host,
		outcome.durable,
	)
	if !ok {
		return hostOutcomePersistence{}, false
	}
	persistence.progress.Generation = generation
	run.hostFailures[outcome.host]++
	if run.hostFailures[outcome.host] < hostFailureRetirementMinimum {
		persistence.progress.Failures = run.hostFailures[outcome.host]
		persistence.persist = true

		return persistence, true
	}
	delete(run.hostFailures, outcome.host)
	run.retiredHosts[outcome.host] = struct{}{}
	persistence.droppedURLs = f.queuedHostURLsLocked(outcome.work.RunID, outcome.host)
	persistence.progress.Retired = true
	persistence.persist = true
	persistence.retire = true

	return persistence, true
}

func (f *Frontier) retireHostQueuedLocked(runID uuid.UUID, host string) (int, *runFinish) {
	settled := f.retireReadyHostLocked(runID, host)
	if run := f.state.runs[runID]; run != nil {
		if run.boundedRecovery {
			run.releaseBoundedPendingHost(host)
		}
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
			if run := f.state.runs[runID]; run != nil {
				run.releaseBoundedResidentPage(job.URL)
			}
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
