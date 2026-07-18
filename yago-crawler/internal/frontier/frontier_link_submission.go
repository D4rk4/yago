package frontier

import (
	"context"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

type discoveredBatchAdmission struct {
	accepted   []frontierCandidate
	duplicates uint64
}

func (f *Frontier) submitCandidateBatch(
	ctx context.Context,
	work crawljob.CrawlJob,
	candidates []frontierCandidate,
) (uint64, bool) {
	run, durable := f.acquireRunDurability(work.RunID)
	state, err := f.loadBoundedAdmissionState(ctx, run, candidates)
	if err != nil {
		f.finishSubmittedBatch(work, run, durable, err)

		return 0, false
	}
	f.mu.Lock()
	if f.state.runs[work.RunID] == run && !runLeaseMatchesJob(run, work) {
		f.finishSubmittedBatchLocked(work, run, durable, nil)

		return 0, false
	}
	admission, err := f.acceptDiscoveredCandidatesLocked(
		ctx,
		work,
		run,
		candidates,
		state,
	)
	if err != nil {
		f.finishSubmittedBatchLocked(work, run, durable, err)

		return 0, false
	}
	f.rebalanceReadyLocked()
	f.mu.Unlock()
	if durable {
		err = f.persistAccepted(ctx, run, admission.accepted)
		f.mu.Lock()
		if err == nil && f.state.runs[work.RunID] == run {
			err = extendBoundedRecovery(run, uint64(len(admission.accepted)))
		}
		f.finishRunDurabilityLocked(work.RunID, run, err)
		f.rebalanceReadyLocked()
		f.mu.Unlock()
	}
	f.wake()

	return admission.duplicates, err == nil
}

func (f *Frontier) acceptDiscoveredCandidatesLocked(
	ctx context.Context,
	work crawljob.CrawlJob,
	run *crawlRun,
	candidates []frontierCandidate,
	state frontiercheckpoint.AdmissionBatchState,
) (discoveredBatchAdmission, error) {
	admission := discoveredBatchAdmission{
		accepted: make([]frontierCandidate, 0, len(candidates)),
	}
	if f.state.runs[work.RunID] != run {
		return admission, nil
	}
	window, err := newBoundedAdmissionWindow(run, state, candidates)
	if err != nil {
		return admission, err
	}
	for index, candidate := range candidates {
		accepted, duplicate := f.acceptWithAdmissionWindowLocked(
			ctx,
			work.RunID,
			run,
			boundedAdmissionCandidate{page: candidate, position: index},
			&window,
		)
		if duplicate {
			admission.duplicates++
		}
		if accepted {
			admission.accepted = append(admission.accepted, candidate)
		}
	}

	return admission, nil
}

func (f *Frontier) finishSubmittedBatch(
	work crawljob.CrawlJob,
	run *crawlRun,
	durable bool,
	err error,
) {
	f.mu.Lock()
	f.finishSubmittedBatchLocked(work, run, durable, err)
}

func (f *Frontier) finishSubmittedBatchLocked(
	work crawljob.CrawlJob,
	run *crawlRun,
	durable bool,
	err error,
) {
	if durable {
		f.finishRunDurabilityLocked(work.RunID, run, err)
	}
	f.mu.Unlock()
	f.wake()
}
