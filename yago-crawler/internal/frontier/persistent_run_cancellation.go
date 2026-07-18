package frontier

import (
	"context"
	"fmt"
	"math"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type queuedPageCancellationCheckpoint interface {
	CancelQueuedPages(context.Context, []byte, []string) error
}

type persistedRunCancellation struct {
	key            string
	run            *crawlRun
	durable        bool
	pageURLs       []string
	recoveryCursor uint64
	recoveryUpper  uint64
	seedRecovery   bool
}

func (f *Frontier) applyRunCancellation(key string) (bool, bool, []runFinish) {
	f.controlOrder.Lock()
	defer f.controlOrder.Unlock()
	f.mu.Lock()
	runID := f.runIDByProvenanceLocked(key)
	f.mu.Unlock()
	run, durable := f.acquireRunDurability(runID)
	if run == nil {
		f.mu.Lock()
		f.applyRunControlLocked(key, cancelControlUpdate())
		f.mu.Unlock()

		return true, false, nil
	}
	checkpointErr := f.persistRunCancellationControl(key, durable)
	f.mu.Lock()
	if durable && checkpointErr != nil {
		f.failProvenanceLocked(key)
		f.finishRunDurabilityLocked(runID, run, checkpointErr)
		f.mu.Unlock()
		f.wake()

		return false, false, nil
	}
	f.applyRunControlLocked(key, cancelControlUpdate())
	if run.seedRecovery {
		run.seedCancelling = true
		run.seedFinishing = false
	}
	seedRecovery := run.seedRecovery
	pageURLs := f.queuedProvenanceURLsLocked(key)
	recoveryCursor := run.recoveryCursor
	recoveryUpper := run.recoveryUpper
	f.mu.Unlock()
	recoveryRemoved, seedManifestDone, checkpointErr := f.persistQueuedRunCancellation(
		persistedRunCancellation{
			key:            key,
			run:            run,
			durable:        durable,
			pageURLs:       pageURLs,
			recoveryCursor: recoveryCursor,
			recoveryUpper:  recoveryUpper,
			seedRecovery:   seedRecovery,
		},
	)
	f.mu.Lock()
	if durable && checkpointErr != nil {
		f.failProvenanceLocked(key)
		f.finishRunDurabilityLocked(runID, run, checkpointErr)
		f.mu.Unlock()
		f.wake()

		return false, false, nil
	}
	finishes := f.cancelQueuedLocked(key)
	f.finishCancelledSeedManifestLocked(runID, run, seedManifestDone)
	if durable {
		f.finishRunDurabilityLocked(runID, run, nil)
	}
	finishes = f.appendPersistedCancellationFinishesLocked(
		runID,
		finishes,
		recoveryRemoved,
		seedManifestDone,
	)
	f.mu.Unlock()
	f.wake()

	return true, durable, finishes
}

func (f *Frontier) finishCancelledSeedManifestLocked(
	runID uuid.UUID,
	run *crawlRun,
	done bool,
) {
	if !done || f.state.runs[runID] != run {
		return
	}
	f.state.tally.Commit(run.provenanceValue, run.seedingTally)
	run.seedingTally = yagocrawlcontract.CrawlRunTally{}
	run.seedRecovery = false
	run.seedFinishing = false
	run.seedCancelling = false
}

func (f *Frontier) persistRunCancellationControl(key string, durable bool) error {
	if !durable {
		return nil
	}
	if err := f.checkpoint.UpdateControl(
		context.Background(),
		[]byte(key),
		checkpointControlUpdate(cancelControlUpdate()),
	); err != nil {
		return fmt.Errorf("persist frontier run cancellation: %w", err)
	}

	return nil
}

func (f *Frontier) runIDByProvenanceLocked(key string) uuid.UUID {
	for candidateID, run := range f.state.runs {
		if run.provenance == key {
			return candidateID
		}
	}

	return uuid.Nil
}

func (f *Frontier) persistQueuedRunCancellation(
	cancellation persistedRunCancellation,
) (uint64, bool, error) {
	if !cancellation.durable {
		return 0, false, nil
	}
	checkpoint, supported := f.checkpoint.(queuedPageCancellationCheckpoint)
	if !supported {
		return 0, false, frontiercheckpoint.ErrCorruptCheckpoint
	}
	checkpointErr := checkpoint.CancelQueuedPages(
		context.Background(),
		[]byte(cancellation.key),
		cancellation.pageURLs,
	)
	recoveryRemoved := uint64(0)
	if checkpointErr == nil && cancellation.run.boundedRecovery &&
		cancellation.recoveryCursor < cancellation.recoveryUpper {
		checkpoint, supported := f.checkpoint.(boundedRecoveryCheckpoint)
		if !supported {
			return 0, false, frontiercheckpoint.ErrCorruptCheckpoint
		}
		recoveryRemoved, checkpointErr = checkpoint.CancelRecoveryPages(
			context.Background(),
			[]byte(cancellation.key),
			cancellation.recoveryCursor,
			cancellation.recoveryUpper,
		)
	}
	seedManifestDone := false
	if checkpointErr == nil && cancellation.seedRecovery {
		checkpoint, supported := f.checkpoint.(boundedRecoveryCheckpoint)
		if !supported {
			return recoveryRemoved, false, frontiercheckpoint.ErrCorruptCheckpoint
		}
		for checkpointErr == nil && !seedManifestDone {
			var err error
			seedManifestDone, err = checkpoint.CancelSeedManifestBatch(
				context.Background(),
				[]byte(cancellation.key),
			)
			if err != nil {
				checkpointErr = fmt.Errorf("cancel frontier seed manifest batch: %w", err)
			}
		}
	}

	return recoveryRemoved, seedManifestDone, checkpointErr
}

func (f *Frontier) appendPersistedCancellationFinishesLocked(
	runID uuid.UUID,
	finishes []runFinish,
	recoveryRemoved uint64,
	seedManifestDone bool,
) []runFinish {
	if recoveryRemoved > uint64(math.MaxInt) {
		f.recordCheckpointFailureLocked(frontiercheckpoint.ErrCorruptCheckpoint)
	} else if recoveryRemoved > 0 {
		if finish := f.settleQueuedManyLocked(runID, int(recoveryRemoved)); finish != nil {
			finishes = append(finishes, *finish)
		}
	}
	if seedManifestDone {
		if finish := f.settleQueuedManyLocked(runID, 1); finish != nil {
			finishes = append(finishes, *finish)
		}
	}

	return finishes
}

func (f *Frontier) queuedProvenanceURLsLocked(provenance string) []string {
	pageURLs := make([]string, 0)
	for _, job := range f.state.ready {
		if string(job.Provenance) == provenance {
			pageURLs = append(pageURLs, job.URL)
		}
	}
	for _, run := range f.state.runs {
		if run.provenance != provenance {
			continue
		}
		for _, bucket := range run.pendingHosts {
			if bucket == nil {
				continue
			}
			for _, page := range bucket.returned[bucket.returnedHead:] {
				pageURLs = append(pageURLs, page.normURL)
			}
			for _, page := range bucket.queued[bucket.queuedHead:] {
				pageURLs = append(pageURLs, page.normURL)
			}
		}
	}

	return pageURLs
}
