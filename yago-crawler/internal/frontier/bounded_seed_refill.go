package frontier

import (
	"context"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type boundedSeedRefill struct {
	runID      uuid.UUID
	cancelling bool
	finishing  bool
	limit      int
}

type boundedSeedAdmissionCompletion struct {
	next           uint64
	complete       bool
	candidateTotal int
	recoveryGrowth uint64
	err            error
}

func (f *Frontier) refillBoundedSeed(ctx context.Context) bool {
	selected, found := f.selectBoundedSeedRefill()
	if !found {
		return false
	}
	run, durable := f.acquireRunDurability(selected.runID)
	if !durable || run == nil {
		f.clearBoundedSeedLoading(run)

		return false
	}
	if selected.cancelling {
		return f.cancelBoundedSeedBatch(ctx, selected.runID, run)
	}
	if selected.finishing {
		return f.finishBoundedSeedBatch(ctx, selected.runID, run)
	}

	return f.admitBoundedSeedBatch(ctx, selected.runID, run, selected.limit)
}

func (f *Frontier) selectBoundedSeedRefill() (boundedSeedRefill, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for runID, run := range f.state.runs {
		if !run.seedRecovery || run.recoveryLoading || run.seeding || run.awaitingDurability {
			continue
		}
		if run.seedCancelling {
			run.recoveryLoading = true

			return boundedSeedRefill{runID: runID, cancelling: true}, true
		}
		if run.seedFinishing {
			run.recoveryLoading = true

			return boundedSeedRefill{runID: runID, finishing: true}, true
		}
		if run.cancelled || !run.recoveryComplete ||
			f.isPausedLocked(run.provenanceValue) || len(run.pageHostProgress) > 0 {
			continue
		}
		live := run.pendingPages + f.readyPerRun[runID]
		if live >= boundedRecoveryLowWatermark {
			continue
		}
		limit := frontiercheckpoint.RecoveryPageBatchSize - live
		run.recoveryLoading = true

		return boundedSeedRefill{runID: runID, limit: limit}, true
	}

	return boundedSeedRefill{}, false
}

func (f *Frontier) admitBoundedSeedBatch(
	ctx context.Context,
	runID uuid.UUID,
	run *crawlRun,
	limit int,
) bool {
	f.mu.Lock()
	if f.state.runs[runID] != run || !run.seedRecovery || run.seedFinishing ||
		run.seedCancelling {
		run.recoveryLoading = false
		f.finishRunDurabilityLocked(runID, run, nil)
		f.mu.Unlock()

		return false
	}
	cursor := run.seedRecoveryCursor
	provenance := append([]byte(nil), run.provenanceValue...)
	f.mu.Unlock()
	candidates, next, complete, err := f.loadBoundedSeedCandidates(
		ctx,
		provenance,
		cursor,
		limit,
	)
	if err != nil {
		f.finishBoundedRecoveryFailure(runID, run, err)

		return false
	}
	admissionState, err := f.loadBoundedAdmissionState(ctx, run, candidates)
	if err != nil {
		f.finishBoundedRecoveryFailure(runID, run, err)

		return false
	}

	f.mu.Lock()
	if f.state.runs[runID] != run {
		run.recoveryLoading = false
		f.finishRunDurabilityLocked(runID, run, nil)
		f.mu.Unlock()

		return false
	}
	admission, err := f.acceptSeedCandidatesLocked(
		ctx,
		runID,
		run,
		candidates,
		admissionState,
	)
	if err != nil {
		run.recoveryLoading = false
		f.finishRunDurabilityLocked(runID, run, err)
		f.mu.Unlock()
		f.wake()

		return false
	}
	f.rebalanceReadyLocked()
	f.mu.Unlock()
	err = f.persistSeedBatch(ctx, run, seedBatchExpectation{
		cursor:     cursor,
		decisions:  admission.decisions,
		admitted:   admission.accepted,
		duplicates: admission.duplicates,
	})

	return f.completeBoundedSeedAdmission(runID, run, boundedSeedAdmissionCompletion{
		next:           next,
		complete:       complete,
		candidateTotal: len(candidates),
		recoveryGrowth: admission.recoveryGrowth,
		err:            err,
	})
}

func (f *Frontier) completeBoundedSeedAdmission(
	runID uuid.UUID,
	run *crawlRun,
	completion boundedSeedAdmissionCompletion,
) bool {
	f.mu.Lock()
	if completion.err == nil && f.state.runs[runID] == run {
		completion.err = extendBoundedRecovery(run, completion.recoveryGrowth)
	}
	if completion.err == nil && f.state.runs[runID] == run {
		run.seedRecoveryCursor = completion.next
		run.seedFinishing = completion.complete
		f.state.tally.Commit(run.provenanceValue, run.seedingTally)
		run.seedingTally = yagocrawlcontract.CrawlRunTally{}
	}
	run.recoveryLoading = false
	f.finishRunDurabilityLocked(runID, run, completion.err)
	f.rebalanceReadyLocked()
	f.mu.Unlock()
	f.wake()

	return completion.err == nil && (completion.candidateTotal > 0 || completion.complete)
}

func (f *Frontier) finishBoundedSeedBatch(
	ctx context.Context,
	runID uuid.UUID,
	run *crawlRun,
) bool {
	checkpoint := f.checkpoint.(boundedRecoveryCheckpoint)
	done, err := checkpoint.FinishSeedingBatch(
		context.WithoutCancel(ctx),
		run.provenanceValue,
		run.seedingTally,
	)

	return f.completeBoundedSeedTransition(runID, run, done, err)
}

func (f *Frontier) cancelBoundedSeedBatch(
	ctx context.Context,
	runID uuid.UUID,
	run *crawlRun,
) bool {
	checkpoint := f.checkpoint.(boundedRecoveryCheckpoint)
	done, err := checkpoint.CancelSeedManifestBatch(
		context.WithoutCancel(ctx),
		run.provenanceValue,
	)

	return f.completeBoundedSeedTransition(runID, run, done, err)
}

func (f *Frontier) completeBoundedSeedTransition(
	runID uuid.UUID,
	run *crawlRun,
	done bool,
	err error,
) bool {
	f.mu.Lock()
	active := f.state.runs[runID] == run
	if err == nil && done && active {
		f.state.tally.Commit(run.provenanceValue, run.seedingTally)
		run.seedingTally = yagocrawlcontract.CrawlRunTally{}
		run.seedRecovery = false
		run.seedFinishing = false
		run.seedCancelling = false
	}
	run.recoveryLoading = false
	f.finishRunDurabilityLocked(runID, run, err)
	var finish *runFinish
	if err == nil && done && active {
		finish = f.settleQueuedManyLocked(runID, 1)
	}
	f.mu.Unlock()
	if finish != nil {
		f.scheduleSettlement(finish.finish, finish.succeeded)
	}
	f.wake()

	return err == nil
}

func (f *Frontier) clearBoundedSeedLoading(run *crawlRun) {
	f.mu.Lock()
	if run != nil {
		run.recoveryLoading = false
	}
	f.mu.Unlock()
}
