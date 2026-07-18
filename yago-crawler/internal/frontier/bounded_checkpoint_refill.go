package frontier

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

const boundedRecoveryLowWatermark = frontierMutationBatchSize / 2

func (f *Frontier) refillBoundedRecovery(ctx context.Context) bool {
	runID, selected := f.selectBoundedRecoveryRun()
	if !selected {
		return false
	}
	load, ready := f.beginBoundedRecoveryLoad(runID)
	if !ready {
		return false
	}
	batch, err := f.loadBoundedRecoveryBatch(ctx, load)
	if err != nil {
		f.finishBoundedRecoveryFailure(runID, load.run, err)

		return false
	}

	return f.applyBoundedRecoveryBatch(load, batch)
}

func (f *Frontier) selectBoundedRecoveryRun() (uuid.UUID, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for runID, run := range f.state.runs {
		if !run.boundedRecovery || run.recoveryComplete || run.recoveryLoading ||
			run.seeding || run.awaitingDurability || run.cancelled ||
			len(run.pageHostProgress) > 0 ||
			f.isPausedLocked(run.provenanceValue) {
			continue
		}
		if run.pendingPages+f.readyPerRun[runID] >= boundedRecoveryLowWatermark {
			continue
		}
		run.recoveryLoading = true

		return runID, true
	}

	return uuid.Nil, false
}

func validateBoundedRecoveryBatch(
	after uint64,
	upper uint64,
	limit int,
	batch frontiercheckpoint.RecoveryPageBatch,
) error {
	retiredPages, err := recoveryPageTotal(batch.RetiredPages)
	if err != nil {
		return err
	}
	if batch.Cursor < after || batch.Cursor > upper ||
		batch.Complete != (batch.Cursor == upper) ||
		(!batch.Complete && batch.Cursor == after) ||
		len(batch.Pages) > limit ||
		retiredPages > limit ||
		len(batch.Pages)+retiredPages > limit {
		return fmt.Errorf(
			"%w: bounded recovery batch is invalid",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}

	return nil
}

func (f *Frontier) appendBoundedRecoveryBatchLocked(
	runID uuid.UUID,
	run *crawlRun,
	batch frontiercheckpoint.RecoveryPageBatch,
) error {
	knownHosts := make(map[string]struct{}, len(run.hostPages))
	for host := range run.hostPages {
		knownHosts[host] = struct{}{}
	}
	if err := restoreCheckpointHostStates(run, batch.HostStates); err != nil {
		return err
	}
	restoredAt := time.Now()
	for _, page := range batch.Pages {
		profile, found := run.profiles[page.ProfileHandle]
		if !found {
			return fmt.Errorf("%w: crawl profile changed", frontiercheckpoint.ErrCorruptCheckpoint)
		}
		if err := validateOutstandingCheckpointPage(page); err != nil {
			return err
		}
		candidate := checkpointCandidate(page, run.provenanceValue)
		run.appendPending(candidate)
		run.retainBoundedResidentPage(page.URL)
		if _, known := knownHosts[page.Host]; !known {
			f.pace.Visited(candidateJob(runID, candidate, profile, run.leaseID), restoredAt)
			knownHosts[page.Host] = struct{}{}
		}
		if page.RedirectURL != "" {
			redirect := redirectReservation{
				URL:      page.RedirectURL,
				Host:     page.RedirectHost,
				HostBump: page.RedirectHostBump,
			}
			run.redirects[page.URL] = redirect
			run.retainBoundedResidentRedirect(redirect)
		}
	}
	run.evictBoundedColdHostStates()

	return nil
}

func checkpointCandidate(page frontiercheckpoint.Page, provenance []byte) frontierCandidate {
	return frontierCandidate{
		normURL:          page.URL,
		host:             page.Host,
		depth:            page.Depth,
		profileHandle:    page.ProfileHandle,
		provenance:       provenance,
		sourceModifiedAt: page.SourceModifiedAt,
		indexAllowed:     page.Index,
		observationID:    page.ObservationID,
		observedAt:       page.ObservedAt,
	}
}

func (f *Frontier) finishBoundedRecoveryFailure(
	runID uuid.UUID,
	run *crawlRun,
	err error,
) {
	f.mu.Lock()
	if run != nil {
		run.recoveryLoading = false
		f.finishRunDurabilityLocked(runID, run, err)
	}
	f.mu.Unlock()
	f.wake()
}
