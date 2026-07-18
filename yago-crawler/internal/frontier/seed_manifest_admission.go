package frontier

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

func seedManifestCandidates(
	snapshot frontiercheckpoint.Snapshot,
	provenance []byte,
) ([]frontierCandidate, uint64) {
	candidates := make([]frontierCandidate, 0, len(snapshot.SeedPages))
	for _, page := range snapshot.SeedPages[snapshot.SeedCursor:] {
		candidates = append(candidates, frontierCandidate{
			normURL:          page.URL,
			host:             page.Host,
			depth:            page.Depth,
			profileHandle:    page.ProfileHandle,
			provenance:       provenance,
			sourceModifiedAt: page.SourceModifiedAt,
			indexAllowed:     page.Index,
			observationID:    page.ObservationID,
			observedAt:       page.ObservedAt,
		})
	}
	return candidates, snapshot.SeedCursor
}

func seedManifestPageCandidates(
	pages []frontiercheckpoint.Page,
	provenance []byte,
) []frontierCandidate {
	candidates := make([]frontierCandidate, 0, len(pages))
	for _, page := range pages {
		candidates = append(candidates, frontierCandidate{
			normURL:          page.URL,
			host:             page.Host,
			depth:            page.Depth,
			profileHandle:    page.ProfileHandle,
			provenance:       provenance,
			sourceModifiedAt: page.SourceModifiedAt,
			indexAllowed:     page.Index,
			observationID:    page.ObservationID,
			observedAt:       page.ObservedAt,
		})
	}

	return candidates
}

func (f *Frontier) loadBoundedSeedCandidates(
	ctx context.Context,
	provenance []byte,
	cursor uint64,
	limit int,
) ([]frontierCandidate, uint64, bool, error) {
	checkpoint := f.checkpoint.(boundedRecoveryCheckpoint)
	pages, next, complete, err := checkpoint.LoadSeedPageBatch(
		context.WithoutCancel(ctx),
		provenance,
		cursor,
		limit,
	)
	if err != nil {
		return nil, 0, false, fmt.Errorf("load frontier seed page batch: %w", err)
	}

	return seedManifestPageCandidates(pages, provenance), next, complete, nil
}

func (f *Frontier) admitSeedCandidateBatch(
	ctx context.Context,
	runID uuid.UUID,
	candidates []frontierCandidate,
	cursor uint64,
) (int, bool) {
	run, durable := f.acquireRunDurability(runID)
	admissionState, admissionErr := f.loadBoundedAdmissionState(ctx, run, candidates)
	if admissionErr != nil {
		f.mu.Lock()
		f.finishRunDurabilityLocked(runID, run, admissionErr)
		f.mu.Unlock()
		f.wake()

		return 0, false
	}
	f.mu.Lock()
	admission, err := f.acceptSeedCandidatesLocked(
		ctx,
		runID,
		run,
		candidates,
		admissionState,
	)
	if err != nil {
		f.finishRunDurabilityLocked(runID, run, err)
		f.mu.Unlock()
		f.wake()

		return 0, false
	}
	f.rebalanceReadyLocked()
	f.mu.Unlock()
	if durable {
		err := f.persistSeedBatch(
			ctx,
			run,
			seedBatchExpectation{
				cursor:     cursor,
				decisions:  admission.decisions,
				admitted:   admission.accepted,
				duplicates: admission.duplicates,
			},
		)
		f.mu.Lock()
		if err == nil && f.state.runs[runID] == run {
			err = extendBoundedRecovery(run, admission.recoveryGrowth)
		}
		f.finishRunDurabilityLocked(runID, run, err)
		f.rebalanceReadyLocked()
		f.mu.Unlock()
		f.wake()
		if err != nil {
			return admission.accepted, false
		}
	}

	return admission.accepted, true
}

type seedBatchAdmission struct {
	decisions      []frontiercheckpoint.SeedDecision
	accepted       int
	recoveryGrowth uint64
	duplicates     uint64
}

func (f *Frontier) acceptSeedCandidatesLocked(
	ctx context.Context,
	runID uuid.UUID,
	run *crawlRun,
	candidates []frontierCandidate,
	state frontiercheckpoint.AdmissionBatchState,
) (seedBatchAdmission, error) {
	admission := seedBatchAdmission{
		decisions: make([]frontiercheckpoint.SeedDecision, 0, len(candidates)),
	}
	if run == nil || f.state.runs[runID] != run {
		return admission, nil
	}
	window, err := newBoundedAdmissionWindow(run, state, candidates)
	if err != nil {
		return seedBatchAdmission{}, err
	}
	for index, candidate := range candidates {
		accepted, duplicate := f.acceptWithAdmissionWindowLocked(
			ctx,
			runID,
			run,
			boundedAdmissionCandidate{page: candidate, position: index},
			&window,
		)
		admission.decisions = append(admission.decisions, frontiercheckpoint.SeedDecision{
			Page:  checkpointPage(candidate),
			Admit: accepted,
		})
		if duplicate {
			run.seedingTally.Duplicates++
			admission.duplicates++
		}
		if accepted {
			admission.accepted++
			admission.recoveryGrowth++
		}
	}

	return admission, nil
}

type seedBatchExpectation struct {
	cursor     uint64
	decisions  []frontiercheckpoint.SeedDecision
	admitted   int
	duplicates uint64
}

func (f *Frontier) persistSeedBatch(
	ctx context.Context,
	run *crawlRun,
	expectation seedBatchExpectation,
) error {
	result, err := f.checkpoint.AdmitSeedBatch(
		context.WithoutCancel(ctx),
		run.provenanceValue,
		frontiercheckpoint.SeedBatch{
			Cursor:    expectation.cursor,
			Decisions: expectation.decisions,
		},
	)
	if err != nil {
		return fmt.Errorf("persist frontier seed batch: %w", err)
	}
	if result.Admitted != expectation.admitted ||
		result.Duplicates != expectation.duplicates {
		return fmt.Errorf(
			"%w: seed batch result admitted %d/%d duplicates %d/%d",
			frontiercheckpoint.ErrCorruptCheckpoint,
			result.Admitted,
			expectation.admitted,
			result.Duplicates,
			expectation.duplicates,
		)
	}
	return nil
}
