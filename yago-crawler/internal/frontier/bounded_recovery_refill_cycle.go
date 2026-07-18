package frontier

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

type boundedRecoveryLoad struct {
	runID      uuid.UUID
	run        *crawlRun
	cursor     uint64
	upper      uint64
	limit      int
	provenance []byte
}

func (f *Frontier) beginBoundedRecoveryLoad(runID uuid.UUID) (boundedRecoveryLoad, bool) {
	run, durable := f.acquireRunDurability(runID)
	if !durable || run == nil {
		f.mu.Lock()
		if run != nil {
			run.recoveryLoading = false
		}
		f.mu.Unlock()

		return boundedRecoveryLoad{}, false
	}
	f.mu.Lock()
	if f.state.runs[runID] != run || !run.boundedRecovery || run.recoveryComplete {
		run.recoveryLoading = false
		f.finishRunDurabilityLocked(runID, run, nil)
		f.mu.Unlock()

		return boundedRecoveryLoad{}, false
	}
	live := run.pendingPages + f.readyPerRun[runID]
	limit := frontiercheckpoint.RecoveryPageBatchSize - live
	if limit <= 0 {
		run.recoveryLoading = false
		f.finishRunDurabilityLocked(runID, run, nil)
		f.mu.Unlock()

		return boundedRecoveryLoad{}, false
	}
	load := boundedRecoveryLoad{
		runID:      runID,
		run:        run,
		cursor:     run.recoveryCursor,
		upper:      run.recoveryUpper,
		limit:      limit,
		provenance: append([]byte(nil), run.provenanceValue...),
	}
	f.mu.Unlock()

	return load, true
}

func (f *Frontier) loadBoundedRecoveryBatch(
	ctx context.Context,
	load boundedRecoveryLoad,
) (frontiercheckpoint.RecoveryPageBatch, error) {
	checkpoint := f.checkpoint.(boundedRecoveryCheckpoint)
	batch, err := checkpoint.LoadRecoveryPageBatch(
		context.WithoutCancel(ctx),
		load.provenance,
		load.cursor,
		load.upper,
		load.limit,
	)
	if err != nil {
		return frontiercheckpoint.RecoveryPageBatch{}, fmt.Errorf(
			"load bounded frontier recovery batch: %w",
			err,
		)
	}
	if err := validateBoundedRecoveryBatch(
		load.cursor,
		load.upper,
		load.limit,
		batch,
	); err != nil {
		return frontiercheckpoint.RecoveryPageBatch{}, err
	}

	return batch, nil
}

func (f *Frontier) applyBoundedRecoveryBatch(
	load boundedRecoveryLoad,
	batch frontiercheckpoint.RecoveryPageBatch,
) bool {
	f.mu.Lock()
	if f.state.runs[load.runID] != load.run {
		load.run.recoveryLoading = false
		f.finishRunDurabilityLocked(load.runID, load.run, nil)
		f.mu.Unlock()

		return false
	}
	if err := f.appendBoundedRecoveryBatchLocked(load.runID, load.run, batch); err != nil {
		load.run.recoveryLoading = false
		f.finishRunDurabilityLocked(load.runID, load.run, err)
		f.mu.Unlock()
		f.wake()

		return false
	}
	load.run.recoveryCursor = batch.Cursor
	load.run.recoveryComplete = batch.Complete
	load.run.recoveryLoading = false
	f.finishRunDurabilityLocked(load.runID, load.run, nil)
	f.rebalanceReadyLocked()
	finish := f.settleRetiredRecoveryPagesLocked(load.runID, batch.RetiredPages)
	f.mu.Unlock()
	if finish != nil {
		f.scheduleSettlement(finish.finish, finish.succeeded)
	}
	f.wake()

	return len(batch.Pages) > 0 || batch.RetiredPages > 0
}

func (f *Frontier) settleRetiredRecoveryPagesLocked(
	runID uuid.UUID,
	retired uint64,
) *runFinish {
	if retired == 0 {
		return nil
	}
	retiredPages, _ := recoveryPageTotal(retired)

	return f.settleQueuedManyLocked(runID, retiredPages)
}
