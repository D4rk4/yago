package frontier

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (f *Frontier) loadCheckpointRun(
	ctx context.Context,
	seed CrawlRunSeed,
	candidates []frontierCandidate,
	profileHandle string,
	state frontiercheckpoint.RunState,
) (frontiercheckpoint.Snapshot, bool, error) {
	if !f.persistent(seed.Provenance) {
		return frontiercheckpoint.Snapshot{}, false, nil
	}
	priority := normalizeCrawlOrderPriority(seed.Priority)
	if state.Status == frontiercheckpoint.RunMissing {
		if err := f.checkpoint.BeginSeedManifest(
			context.WithoutCancel(ctx),
			seed.Provenance,
			seed.OrderIdentity,
			priority,
			checkpointPages(candidates),
		); err != nil {
			return frontiercheckpoint.Snapshot{}, true, fmt.Errorf(
				"begin frontier seed manifest: %w",
				err,
			)
		}
	}
	var snapshot frontiercheckpoint.Snapshot
	var err error
	bounded, supportsBounded := f.checkpoint.(boundedRecoveryCheckpoint)
	if supportsBounded {
		snapshot, err = bounded.LoadBounded(
			context.WithoutCancel(ctx),
			seed.Provenance,
			frontierMutationBatchSize,
		)
	} else {
		snapshot, err = f.checkpoint.Load(context.WithoutCancel(ctx), seed.Provenance)
	}
	if err != nil {
		return frontiercheckpoint.Snapshot{}, true, fmt.Errorf(
			"load frontier checkpoint: %w",
			err,
		)
	}
	if snapshot.RecoveryBounded && !supportsBounded {
		return frontiercheckpoint.Snapshot{}, true, fmt.Errorf(
			"%w: bounded recovery checkpoint is unavailable",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	if err := validateCheckpointSnapshot(snapshot, seed, priority, profileHandle); err != nil {
		return frontiercheckpoint.Snapshot{}, true, err
	}

	return snapshot, true, nil
}

func validateCheckpointSnapshot(
	snapshot frontiercheckpoint.Snapshot,
	seed CrawlRunSeed,
	priority yagocrawlcontract.CrawlOrderPriority,
	profileHandle string,
) error {
	if string(snapshot.OrderIdentity) != string(seed.OrderIdentity) ||
		snapshot.Priority != priority {
		return fmt.Errorf("%w: run identity changed", frontiercheckpoint.ErrCorruptCheckpoint)
	}
	if snapshot.Counters.Pages > uint64(math.MaxInt) ||
		snapshot.Counters.Pending > uint64(math.MaxInt) {
		return fmt.Errorf(
			"%w: run page total exceeds platform capacity",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	if snapshot.Counters.Pages < snapshot.Counters.Pending ||
		snapshot.BudgetDiscardedPages > snapshot.Counters.Pages-snapshot.Counters.Pending {
		return fmt.Errorf(
			"%w: run page budget accounting is invalid",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	if snapshot.RecoveryBounded &&
		(snapshot.RecoveryCursor > snapshot.RecoveryUpper ||
			snapshot.RecoveryComplete != (snapshot.RecoveryCursor == snapshot.RecoveryUpper) ||
			len(snapshot.Outstanding) > frontiercheckpoint.RecoveryPageBatchSize) {
		return fmt.Errorf(
			"%w: bounded recovery state is invalid",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	for host, state := range snapshot.HostStates {
		if host == "" || state.Pages > uint64(math.MaxInt) {
			return fmt.Errorf("%w: host state is invalid", frontiercheckpoint.ErrCorruptCheckpoint)
		}
	}
	for _, page := range snapshot.Outstanding {
		if err := validateOutstandingCheckpointPage(page); err != nil {
			return err
		}
	}
	for _, page := range snapshot.SeedPages {
		if err := validateCheckpointPage(page, profileHandle); err != nil {
			return err
		}
	}

	return nil
}

func validateOutstandingCheckpointPage(page frontiercheckpoint.Page) error {
	normalized, ok := weburl.Normalize(page.URL)
	if !ok || normalized != page.URL || weburl.Host(page.URL) != page.Host ||
		page.ProfileHandle == "" || page.ObservationID == "" || page.ObservedAt.IsZero() {
		return fmt.Errorf(
			"%w: outstanding page is invalid",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	if page.RedirectURL == "" {
		if page.RedirectHost != "" || page.RedirectHostBump {
			return fmt.Errorf(
				"%w: redirect state is invalid",
				frontiercheckpoint.ErrCorruptCheckpoint,
			)
		}

		return nil
	}
	redirect, ok := weburl.Normalize(page.RedirectURL)
	if !ok || redirect != page.RedirectURL || weburl.Host(page.RedirectURL) != page.RedirectHost ||
		page.RedirectHostBump != (page.RedirectHost != page.Host) {
		return fmt.Errorf("%w: redirect state is invalid", frontiercheckpoint.ErrCorruptCheckpoint)
	}

	return nil
}

func validateCheckpointPage(page frontiercheckpoint.Page, expectedProfile string) error {
	normalized, ok := weburl.Normalize(page.URL)
	if !ok || normalized != page.URL || weburl.Host(page.URL) != page.Host ||
		page.ProfileHandle == "" || page.ObservationID == "" || page.ObservedAt.IsZero() ||
		page.ProfileHandle != expectedProfile || page.RedirectURL != "" ||
		page.RedirectHost != "" || page.RedirectHostBump {
		return fmt.Errorf(
			"%w: seed manifest page is invalid",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	return nil
}

func (f *Frontier) restoreCheckpointRunLocked(
	runID uuid.UUID,
	snapshot frontiercheckpoint.Snapshot,
	profile crawladmission.AdmissionProfile,
) error {
	run := f.state.runs[runID]
	pages, _ := platformPageTotal(snapshot.Counters.Pages)
	pending, _ := platformPageTotal(snapshot.Counters.Pending)
	run.priority = snapshot.Priority
	run.pages = pages
	run.boundedRecovery = snapshot.RecoveryBounded
	run.recoveryCursor = snapshot.RecoveryCursor
	run.recoveryUpper = snapshot.RecoveryUpper
	run.recoveryComplete = snapshot.RecoveryComplete
	run.seedRecovery = snapshot.RecoveryBounded && snapshot.Seeding
	run.seedRecoveryCursor = snapshot.SeedCursor
	run.seedRecoveryLength = snapshot.SeedLength
	if !snapshot.RecoveryBounded {
		for pageURL := range snapshot.Visited {
			run.visited[pageURL] = struct{}{}
		}
	}
	restoreValidatedCheckpointHostStates(run, snapshot.HostStates)
	if snapshot.RecoveryBounded &&
		!f.state.completion.TrackMany(runID, pending) {
		return fmt.Errorf(
			"%w: pending recovery total is invalid",
			frontiercheckpoint.ErrCorruptCheckpoint,
		)
	}
	restoredAt := time.Now()
	restoredHosts := make(map[string]struct{})
	for _, page := range snapshot.Outstanding {
		if page.ProfileHandle != profile.Profile.Handle {
			return fmt.Errorf("%w: crawl profile changed", frontiercheckpoint.ErrCorruptCheckpoint)
		}
		candidate := frontierCandidate{
			normURL:          page.URL,
			host:             page.Host,
			depth:            page.Depth,
			profileHandle:    page.ProfileHandle,
			provenance:       run.provenanceValue,
			sourceModifiedAt: page.SourceModifiedAt,
			indexAllowed:     page.Index,
			observationID:    page.ObservationID,
			observedAt:       page.ObservedAt,
		}
		run.appendPending(candidate)
		run.retainBoundedResidentPage(page.URL)
		if _, restored := restoredHosts[page.Host]; !restored {
			f.pace.Visited(candidateJob(runID, candidate, profile, run.leaseID), restoredAt)
			restoredHosts[page.Host] = struct{}{}
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
		if !snapshot.RecoveryBounded {
			f.state.completion.Track(runID)
		}
	}
	if snapshot.RecoveryBounded {
		run.evictBoundedColdHostStates()
	}
	if snapshot.Failed {
		snapshot.Tally.Failed = max(snapshot.Tally.Failed, 1)
	}
	f.state.tally.Restore(run.provenanceValue, snapshot.Tally)
	f.restoreControlStateLocked(runID, snapshot.Control)

	return nil
}

func restoreCheckpointHostStates(
	run *crawlRun,
	states map[string]frontiercheckpoint.HostState,
) error {
	for host, state := range states {
		if host == "" || state.Pages > uint64(math.MaxInt) {
			return fmt.Errorf("%w: host state is invalid", frontiercheckpoint.ErrCorruptCheckpoint)
		}
	}
	restoreValidatedCheckpointHostStates(run, states)

	return nil
}

func restoreValidatedCheckpointHostStates(
	run *crawlRun,
	states map[string]frontiercheckpoint.HostState,
) {
	for host, state := range states {
		if _, known := run.hostPages[host]; known {
			continue
		}
		pages, _ := platformPageTotal(state.Pages)
		run.hostPages[host] = pages
		run.hostGenerations[host] = state.Generation
		if state.Failures > 0 {
			run.hostFailures[host] = state.Failures
		} else {
			delete(run.hostFailures, host)
		}
		if state.Retired {
			run.retiredHosts[host] = struct{}{}
		} else {
			delete(run.retiredHosts, host)
		}
	}
}

func (f *Frontier) persistAccepted(
	ctx context.Context,
	run *crawlRun,
	candidates []frontierCandidate,
) error {
	if len(candidates) == 0 {
		return nil
	}
	pages := make([]frontiercheckpoint.Page, 0, len(candidates))
	for _, candidate := range candidates {
		pages = append(pages, checkpointPage(candidate))
	}
	admitted, err := f.checkpoint.Admit(
		context.WithoutCancel(ctx),
		run.provenanceValue,
		pages,
	)
	if err != nil {
		return fmt.Errorf("persist accepted frontier pages: %w", err)
	}
	if admitted != len(pages) {
		return fmt.Errorf(
			"%w: admitted %d of %d accepted pages",
			frontiercheckpoint.ErrCorruptCheckpoint,
			admitted,
			len(pages),
		)
	}
	return nil
}

func checkpointPage(candidate frontierCandidate) frontiercheckpoint.Page {
	return frontiercheckpoint.Page{
		URL:              candidate.normURL,
		Host:             candidate.host,
		Depth:            candidate.depth,
		ProfileHandle:    candidate.profileHandle,
		ObservationID:    candidate.observationID,
		ObservedAt:       candidate.observedAt,
		SourceModifiedAt: candidate.sourceModifiedAt,
		Index:            candidate.indexAllowed,
	}
}

func checkpointPages(candidates []frontierCandidate) []frontiercheckpoint.Page {
	pages := make([]frontiercheckpoint.Page, 0, len(candidates))
	for _, candidate := range candidates {
		pages = append(pages, checkpointPage(candidate))
	}
	return pages
}

func (f *Frontier) finishCheckpointSeeding(
	ctx context.Context,
	provenance []byte,
	seeding bool,
	tally yagocrawlcontract.CrawlRunTally,
) error {
	if !seeding {
		return nil
	}

	if err := f.checkpoint.FinishSeeding(
		context.WithoutCancel(ctx),
		provenance,
		tally,
	); err != nil {
		return fmt.Errorf("finish frontier checkpoint seeding: %w", err)
	}

	return nil
}
