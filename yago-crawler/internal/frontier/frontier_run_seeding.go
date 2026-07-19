package frontier

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type runCheckpointPreparation struct {
	candidates []frontierCandidate
	snapshot   frontiercheckpoint.Snapshot
	persistent bool
}

type runSeedAdmissionResult struct {
	queued      int
	cursor      uint64
	lazySeeding bool
	continued   bool
}

func (f *Frontier) prepareRunCheckpoint(
	ctx context.Context,
	seed CrawlRunSeed,
	profile crawladmission.AdmissionProfile,
) (runCheckpointPreparation, error) {
	prepareCandidates := true
	var checkpointState frontiercheckpoint.RunState
	if f.persistent(seed.Provenance) {
		state, err := f.checkpoint.Inspect(
			context.WithoutCancel(ctx),
			seed.Provenance,
			seed.OrderIdentity,
		)
		if err != nil {
			return runCheckpointPreparation{}, fmt.Errorf(
				"inspect crawl run seed checkpoint: %w",
				err,
			)
		}
		checkpointState = state
		prepareCandidates = state.Status == frontiercheckpoint.RunMissing
	}
	var candidates []frontierCandidate
	if prepareCandidates {
		candidates = f.prepareSeeds(ctx, seed.Requests, seed.Provenance, profile)
	}
	snapshot, persistent, err := f.loadCheckpointRun(
		ctx,
		seed,
		candidates,
		profile.Profile.Handle,
		checkpointState,
	)
	if err != nil {
		return runCheckpointPreparation{}, err
	}
	snapshot, err = f.enforceRecoveredRunPageBudget(ctx, seed, profile, snapshot, persistent)
	if err != nil {
		return runCheckpointPreparation{}, err
	}

	return runCheckpointPreparation{
		candidates: candidates,
		snapshot:   snapshot,
		persistent: persistent,
	}, nil
}

func (f *Frontier) activateSeedRun(
	runID uuid.UUID,
	seed CrawlRunSeed,
	profile crawladmission.AdmissionProfile,
	finish func(bool),
	preparation runCheckpointPreparation,
) {
	order := yagocrawlcontract.CrawlOrder{
		Priority: seed.Priority,
		Profile:  profile.Profile,
	}
	maximum := order.EffectiveMaxPagesPerRun(f.state.maxPagesPerRun)
	profile.Profile.MaxPagesPerRun = &maximum
	f.controlOrder.Lock()
	f.mu.Lock()
	f.state.beginRun(runID, seed.Provenance, profile, finish)
	f.state.runs[runID].leaseID = seed.LeaseID
	if preparation.persistent {
		if err := f.restoreCheckpointRunLocked(
			runID,
			preparation.snapshot,
			profile,
		); err != nil {
			f.state.completion.Fail(runID)
			f.recordCheckpointFailureLocked(err)
		}
	} else {
		f.state.runs[runID].priority = normalizeCrawlOrderPriority(seed.Priority)
	}
	f.mu.Unlock()
	f.persistPendingControl(runID)
	f.controlOrder.Unlock()
}

func (f *Frontier) admitPreparedRunSeeds(
	ctx context.Context,
	runID uuid.UUID,
	seed CrawlRunSeed,
	preparation runCheckpointPreparation,
) (runSeedAdmissionResult, error) {
	queued := 0
	if preparation.persistent {
		var err error
		queued, err = platformPageTotal(preparation.snapshot.Counters.Pending)
		if err != nil {
			return runSeedAdmissionResult{}, err
		}
	}
	cursor := uint64(0)
	if preparation.persistent {
		cursor = preparation.snapshot.SeedCursor
	}
	result := runSeedAdmissionResult{queued: queued, cursor: cursor, continued: true}
	if preparation.persistent && preparation.snapshot.Seeding &&
		preparation.snapshot.RecoveryBounded {
		return f.admitLazyRunSeeds(ctx, runID, seed, preparation, result)
	}

	return f.admitManifestRunSeeds(ctx, runID, seed, preparation, result)
}

func (f *Frontier) admitLazyRunSeeds(
	ctx context.Context,
	runID uuid.UUID,
	seed CrawlRunSeed,
	preparation runCheckpointPreparation,
	result runSeedAdmissionResult,
) (runSeedAdmissionResult, error) {
	capacity := frontiercheckpoint.RecoveryPageBatchSize - len(preparation.snapshot.Outstanding)
	if result.cursor < preparation.snapshot.SeedLength && capacity > 0 {
		candidates, nextCursor, _, err := f.loadBoundedSeedCandidates(
			ctx,
			seed.Provenance,
			result.cursor,
			capacity,
		)
		if err != nil {
			return result, err
		}
		accepted, continued := f.admitSeedCandidateBatch(
			ctx,
			runID,
			candidates,
			result.cursor,
		)
		result.queued += accepted
		result.continued = continued
		result.cursor = nextCursor
	}
	result.lazySeeding = result.cursor < preparation.snapshot.SeedLength

	return result, nil
}

func (f *Frontier) admitManifestRunSeeds(
	ctx context.Context,
	runID uuid.UUID,
	seed CrawlRunSeed,
	preparation runCheckpointPreparation,
	result runSeedAdmissionResult,
) (runSeedAdmissionResult, error) {
	candidates := preparation.candidates
	if preparation.persistent {
		if !preparation.snapshot.Seeding {
			return result, nil
		}
		candidates, result.cursor = seedManifestCandidates(
			preparation.snapshot,
			seed.Provenance,
		)
	}
	for start := 0; start < len(candidates); start += frontierMutationBatchSize {
		end := min(start+frontierMutationBatchSize, len(candidates))
		accepted, continued := f.admitSeedCandidateBatch(
			ctx,
			runID,
			candidates[start:end],
			result.cursor,
		)
		result.queued += accepted
		result.continued = continued
		if !continued {
			return result, nil
		}
		advance, _ := seedCursorAdvance(end - start)
		result.cursor += advance
	}

	return result, nil
}

func (f *Frontier) finishPreparedRunSeeding(
	ctx context.Context,
	runID uuid.UUID,
	seed CrawlRunSeed,
	preparation runCheckpointPreparation,
	result runSeedAdmissionResult,
) SeededRun {
	var run *crawlRun
	durable := false
	var seedingError error
	if !result.lazySeeding {
		run, durable = f.acquireRunDurability(runID)
		if durable {
			seedingError = f.finishCheckpointSeeding(
				ctx,
				seed.Provenance,
				preparation.persistent && preparation.snapshot.Seeding,
				run.seedingTally,
			)
		}
	}
	f.mu.Lock()
	if durable {
		f.finishRunDurabilityLocked(runID, run, seedingError)
	}
	activeRun := f.state.runs[runID]
	if activeRun == nil {
		f.mu.Unlock()
		f.wake()

		return SeededRun{RunID: runID, Queued: result.queued}
	}
	f.state.tally.Commit(activeRun.provenanceValue, activeRun.seedingTally)
	activeRun.seedingTally = yagocrawlcontract.CrawlRunTally{}
	activeRun.seeding = false
	f.finishSeedRecoveryLocked(activeRun, result)
	controlFinishes := []runFinish(nil)
	if activeRun.cancelled {
		controlFinishes = f.cancelQueuedLocked(activeRun.provenance)
	}
	f.demoteControlBlockedReadyLocked()
	f.rebalanceReadyLocked()
	f.refillReadyLocked()
	settled, succeeded, drained := f.settleSeededRunLocked(runID, result.lazySeeding)
	f.mu.Unlock()
	f.wake()
	if preparation.persistent && preparation.snapshot.Counters.Pages == 0 &&
		preparation.snapshot.RecoveryUpper == 0 && result.queued > 0 {
		f.refillBoundedRecovery(ctx)
	}
	f.scheduleSettlements(controlFinishes)
	if drained && settled != nil {
		f.scheduleSettlement(settled, succeeded)
	}

	return SeededRun{RunID: runID, Queued: result.queued}
}

func (f *Frontier) finishSeedRecoveryLocked(
	run *crawlRun,
	result runSeedAdmissionResult,
) {
	if result.lazySeeding {
		run.seedRecoveryCursor = result.cursor
		run.seedFinishing = result.cursor == run.seedRecoveryLength

		return
	}
	run.seedRecovery = false
	run.seedFinishing = false
	run.seedCancelling = false
}

func (f *Frontier) settleSeededRunLocked(
	runID uuid.UUID,
	lazySeeding bool,
) (func(bool), bool, bool) {
	if lazySeeding {
		return nil, false, false
	}
	settled, succeeded, drained := f.state.completion.Settle(runID)
	if drained {
		f.cleanupRunLocked(runID)
	}

	return settled, succeeded, drained
}
