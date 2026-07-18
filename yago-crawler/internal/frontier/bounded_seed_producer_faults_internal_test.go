package frontier

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
)

func prepareBoundedSeedRun(
	t *testing.T,
	checkpoint *boundedCheckpointScript,
) (*Frontier, uuid.UUID, *crawlRun) {
	t.Helper()
	frontier, runID, run, _ := beginBoundedProducerRun(t, checkpoint)
	frontier.mu.Lock()
	run.recoveryComplete = true
	run.seedRecovery = true
	run.seedRecoveryLength = 2
	frontier.mu.Unlock()

	return frontier, runID, run
}

func TestBoundedSeedRefillSelectionHonorsControlAndTransitions(t *testing.T) {
	blocked := []struct {
		name  string
		block func(*Frontier, *crawlRun)
	}{
		{name: "cancelled", block: func(_ *Frontier, run *crawlRun) { run.cancelled = true }},
		{name: "recovery incomplete", block: func(_ *Frontier, run *crawlRun) {
			run.recoveryComplete = false
		}},
		{name: "paused", block: func(frontier *Frontier, run *crawlRun) {
			frontier.paused[run.provenance] = struct{}{}
		}},
		{name: "host progress", block: func(_ *Frontier, run *crawlRun) {
			run.pageHostProgress["observation"] = stagedPageHostProgress{}
		}},
	}
	for _, testCase := range blocked {
		t.Run(testCase.name, func(t *testing.T) {
			frontier, _, run := prepareBoundedSeedRun(t, &boundedCheckpointScript{})
			frontier.mu.Lock()
			testCase.block(frontier, run)
			frontier.mu.Unlock()
			if _, selected := frontier.selectBoundedSeedRefill(); selected {
				t.Fatal("blocked seed producer was selected")
			}
		})
	}

	cancelling, cancellingID, cancellingRun := prepareBoundedSeedRun(
		t, &boundedCheckpointScript{},
	)
	cancelling.mu.Lock()
	cancellingRun.seedCancelling = true
	cancelling.mu.Unlock()
	selected, found := cancelling.selectBoundedSeedRefill()
	if !found || selected.runID != cancellingID || !selected.cancelling {
		t.Fatalf("cancelling seed selection = %+v, %t", selected, found)
	}

	finishing, finishingID, finishingRun := prepareBoundedSeedRun(
		t, &boundedCheckpointScript{},
	)
	finishing.mu.Lock()
	finishingRun.seedFinishing = true
	finishing.mu.Unlock()
	selected, found = finishing.selectBoundedSeedRefill()
	if !found || selected.runID != finishingID || !selected.finishing {
		t.Fatalf("finishing seed selection = %+v, %t", selected, found)
	}
}

func TestBoundedSeedRefillRoutesCancellationFinishAndLostDurability(t *testing.T) {
	cancelCheckpoint := &boundedCheckpointScript{cancelSeedDone: false}
	cancelling, _, cancellingRun := prepareBoundedSeedRun(t, cancelCheckpoint)
	cancelling.mu.Lock()
	cancellingRun.seedCancelling = true
	cancelling.mu.Unlock()
	if !cancelling.refillBoundedSeed(t.Context()) {
		t.Fatal("successful cancellation batch did not report progress")
	}
	if !cancellingRun.seedCancelling || cancellingRun.recoveryLoading {
		t.Fatalf(
			"partial cancellation state cancelling=%t loading=%t",
			cancellingRun.seedCancelling,
			cancellingRun.recoveryLoading,
		)
	}

	finished := make(chan bool, 1)
	finishCheckpoint := &boundedCheckpointScript{finishSeedingDone: true}
	finishing, finishingID, finishingRun := prepareBoundedSeedRun(t, finishCheckpoint)
	finishing.mu.Lock()
	finishing.state.completion.Settle(finishingID)
	finishing.state.completion.Begin(finishingID, func(succeeded bool) { finished <- succeeded })
	finishingRun.seedFinishing = true
	finishing.mu.Unlock()
	if !finishing.refillBoundedSeed(t.Context()) {
		t.Fatal("completed seed manifest did not report progress")
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("completed seed manifest failed")
		}
	case <-time.After(time.Second):
		t.Fatal("completed seed manifest did not settle")
	}

	profile := internalProfile(t)
	volatile := NewFrontier(2, nil)
	volatileID := uuid.New()
	volatile.mu.Lock()
	volatile.state.beginRun(volatileID, []byte("volatile-seed"), profile, nil)
	volatileRun := volatile.state.runs[volatileID]
	volatileRun.seeding = false
	volatileRun.seedRecovery = true
	volatileRun.recoveryComplete = true
	volatile.mu.Unlock()
	if volatile.refillBoundedSeed(t.Context()) {
		t.Fatal("volatile seed producer reported progress")
	}
	if volatileRun.recoveryLoading {
		t.Fatal("volatile seed producer remained loading")
	}
	volatile.clearBoundedSeedLoading(nil)
}

func TestBoundedSeedAdmissionFencesFaultsAndConcurrentRemoval(t *testing.T) {
	pageProfile := internalProfile(t)
	page := boundedProducerPage(
		pageProfile.Profile.Handle,
		"https://recovery.example/seed",
	)

	invalidCheckpoint := &boundedCheckpointScript{}
	invalid, invalidID, invalidRun := prepareBoundedSeedRun(t, invalidCheckpoint)
	lockProducerRunDurability(invalidRun)
	invalid.mu.Lock()
	invalidRun.seedFinishing = true
	invalidRun.recoveryLoading = true
	invalid.mu.Unlock()
	if invalid.admitBoundedSeedBatch(t.Context(), invalidID, invalidRun, 1) {
		t.Fatal("finishing seed run admitted another batch")
	}

	loadFailure := errors.New("load seed pages")
	loadCheckpoint := &boundedCheckpointScript{seedPagesError: loadFailure}
	loadFrontier, _, _ := prepareBoundedSeedRun(t, loadCheckpoint)
	if loadFrontier.refillBoundedSeed(t.Context()) {
		t.Fatal("failed seed-page load reported progress")
	}
	if !errors.Is(loadFrontier.CheckpointFailure(), loadFailure) {
		t.Fatalf("seed-page load failure = %v", loadFrontier.CheckpointFailure())
	}

	stateFailure := errors.New("load seed admission state")
	stateCheckpoint := &boundedCheckpointScript{
		seedPages: []frontiercheckpoint.Page{page}, seedNext: 1,
		admissionStateError: stateFailure,
	}
	stateFrontier, _, _ := prepareBoundedSeedRun(t, stateCheckpoint)
	if stateFrontier.refillBoundedSeed(t.Context()) {
		t.Fatal("failed admission-state load reported progress")
	}
	if !errors.Is(stateFrontier.CheckpointFailure(), stateFailure) {
		t.Fatalf("admission-state failure = %v", stateFrontier.CheckpointFailure())
	}

	removedCheckpoint := &boundedCheckpointScript{
		seedPages: []frontiercheckpoint.Page{page}, seedNext: 1,
		admissionState: frontiercheckpoint.AdmissionBatchState{Visited: []bool{false}},
	}
	removedFrontier, removedID, _ := prepareBoundedSeedRun(t, removedCheckpoint)
	removedCheckpoint.onSeedLoad = func() {
		removedFrontier.mu.Lock()
		delete(removedFrontier.state.runs, removedID)
		removedFrontier.mu.Unlock()
	}
	if removedFrontier.refillBoundedSeed(t.Context()) {
		t.Fatal("removed seed run admitted work")
	}

	shapeCheckpoint := &boundedCheckpointScript{
		seedPages: []frontiercheckpoint.Page{page}, seedNext: 1,
		admissionState: frontiercheckpoint.AdmissionBatchState{},
	}
	shapeFrontier, _, _ := prepareBoundedSeedRun(t, shapeCheckpoint)
	if shapeFrontier.refillBoundedSeed(t.Context()) {
		t.Fatal("invalid admission shape reported progress")
	}
	if !errors.Is(shapeFrontier.CheckpointFailure(), frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("admission shape failure = %v", shapeFrontier.CheckpointFailure())
	}
}

func TestBoundedSeedWriteFailureDoesNotExtendRecovery(t *testing.T) {
	writeFailure := errors.New("persist seed batch")
	profile := internalProfile(t)
	checkpoint := &boundedCheckpointScript{
		seedPages: []frontiercheckpoint.Page{
			boundedProducerPage(profile.Profile.Handle, "https://recovery.example/seed"),
		},
		seedNext:       1,
		admissionState: frontiercheckpoint.AdmissionBatchState{Visited: []bool{false}},
	}
	checkpoint.admissionError = writeFailure
	frontier, _, run := prepareBoundedSeedRun(t, checkpoint)
	if frontier.refillBoundedSeed(t.Context()) {
		t.Fatal("failed seed batch write reported progress")
	}
	if !errors.Is(frontier.CheckpointFailure(), writeFailure) {
		t.Fatalf("seed write failure = %v", frontier.CheckpointFailure())
	}
	if run.recoveryUpper != 10 {
		t.Fatalf("failed seed write recovery upper = %d", run.recoveryUpper)
	}
}

func TestBoundedSeedTransitionsRetainPartialStateAndFenceRemovedRuns(t *testing.T) {
	partialCheckpoint := &boundedCheckpointScript{}
	partial, partialID, partialRun := prepareBoundedSeedRun(t, partialCheckpoint)
	lockProducerRunDurability(partialRun)
	partialRun.recoveryLoading = true
	partialRun.seedFinishing = true
	if !partial.completeBoundedSeedTransition(partialID, partialRun, false, nil) {
		t.Fatal("partial seed transition reported failure")
	}
	if !partialRun.seedFinishing || partialRun.recoveryLoading {
		t.Fatalf(
			"partial transition finishing=%t loading=%t",
			partialRun.seedFinishing,
			partialRun.recoveryLoading,
		)
	}

	transitionFailure := errors.New("finish seed transition")
	failed, failedID, failedRun := prepareBoundedSeedRun(t, &boundedCheckpointScript{})
	lockProducerRunDurability(failedRun)
	failedRun.recoveryLoading = true
	if failed.completeBoundedSeedTransition(
		failedID, failedRun, true, transitionFailure,
	) {
		t.Fatal("failed seed transition reported success")
	}
	if !errors.Is(failed.CheckpointFailure(), transitionFailure) {
		t.Fatalf("seed transition failure = %v", failed.CheckpointFailure())
	}

	removed, removedID, removedRun := prepareBoundedSeedRun(t, &boundedCheckpointScript{})
	lockProducerRunDurability(removedRun)
	removedRun.recoveryLoading = true
	removed.mu.Lock()
	delete(removed.state.runs, removedID)
	removed.mu.Unlock()
	if !removed.completeBoundedSeedTransition(removedID, removedRun, true, nil) {
		t.Fatal("removed seed transition reported storage failure")
	}
}

func TestSeedBatchAdmissionReportsReadAndValidationFailures(t *testing.T) {
	profile := internalProfile(t)
	page := boundedProducerPage(
		profile.Profile.Handle,
		"https://recovery.example/admit",
	)
	candidate := checkpointCandidate(page, []byte("seed-admission"))

	readFailure := errors.New("read candidate state")
	readCheckpoint := &boundedCheckpointScript{admissionStateError: readFailure}
	readFrontier, readID, _, _ := beginBoundedProducerRun(t, readCheckpoint)
	if accepted, continued := readFrontier.admitSeedCandidateBatch(
		t.Context(), readID, []frontierCandidate{candidate}, 0,
	); accepted != 0 || continued {
		t.Fatalf("failed admission accepted=%d continued=%t", accepted, continued)
	}
	if !errors.Is(readFrontier.CheckpointFailure(), readFailure) {
		t.Fatalf("candidate-state failure = %v", readFrontier.CheckpointFailure())
	}

	shapeCheckpoint := &boundedCheckpointScript{
		admissionState: frontiercheckpoint.AdmissionBatchState{},
	}
	shapeFrontier, shapeID, _, _ := beginBoundedProducerRun(t, shapeCheckpoint)
	if accepted, continued := shapeFrontier.admitSeedCandidateBatch(
		t.Context(), shapeID, []frontierCandidate{candidate}, 0,
	); accepted != 0 || continued {
		t.Fatalf("invalid admission accepted=%d continued=%t", accepted, continued)
	}
	if !errors.Is(shapeFrontier.CheckpointFailure(), frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("candidate shape failure = %v", shapeFrontier.CheckpointFailure())
	}

	staleFrontier, staleID, staleRun, _ := beginBoundedProducerRun(
		t, &boundedCheckpointScript{},
	)
	staleFrontier.mu.Lock()
	delete(staleFrontier.state.runs, staleID)
	admission, err := staleFrontier.acceptSeedCandidatesLocked(
		t.Context(), staleID, staleRun, []frontierCandidate{candidate},
		frontiercheckpoint.AdmissionBatchState{},
	)
	staleFrontier.mu.Unlock()
	if err != nil || admission.accepted != 0 || len(admission.decisions) != 0 {
		t.Fatalf("stale candidate admission = %+v, %v", admission, err)
	}
}

func TestPrepareAndLazySeedRecoveryPropagateReadFailures(t *testing.T) {
	profile := internalProfile(t)
	inspectFailure := errors.New("inspect seed checkpoint")
	checkpoint := &scriptedCheckpoint{statusError: inspectFailure}
	frontier := NewFrontier(2, nil, WithCheckpoint(checkpoint))
	if _, err := frontier.prepareRunCheckpoint(
		t.Context(),
		CrawlRunSeed{Provenance: []byte("inspect"), OrderIdentity: []byte("identity")},
		profile,
	); !errors.Is(err, inspectFailure) {
		t.Fatalf("prepare inspect error = %v", err)
	}

	lazyFailure := errors.New("load lazy seed batch")
	lazyCheckpoint := &boundedCheckpointScript{seedPagesError: lazyFailure}
	lazyFrontier, runID, _, _ := beginBoundedProducerRun(t, lazyCheckpoint)
	preparation := runCheckpointPreparation{
		persistent: true,
		snapshot: frontiercheckpoint.Snapshot{
			RecoveryBounded: true,
			Seeding:         true,
			SeedLength:      2,
		},
	}
	_, err := lazyFrontier.admitLazyRunSeeds(
		t.Context(),
		runID,
		CrawlRunSeed{Provenance: []byte("lazy")},
		preparation,
		runSeedAdmissionResult{continued: true},
	)
	if !errors.Is(err, lazyFailure) {
		t.Fatalf("lazy seed load error = %v", err)
	}
}

func TestAdmitPreparedSeedsRejectsUnrepresentablePendingTotal(t *testing.T) {
	frontier := NewFrontier(2, nil)
	_, err := frontier.admitPreparedRunSeeds(
		t.Context(),
		uuid.New(),
		CrawlRunSeed{},
		runCheckpointPreparation{
			persistent: true,
			snapshot: frontiercheckpoint.Snapshot{
				Counters: frontiercheckpoint.Counters{Pending: uint64(math.MaxInt) + 1},
			},
		},
	)
	if !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("pending total error = %v", err)
	}
}

func TestFinishPreparedSeedingHandlesConcurrentRunRemoval(t *testing.T) {
	frontier := NewFrontier(2, nil)
	runID := uuid.New()
	seeded := frontier.finishPreparedRunSeeding(
		t.Context(),
		runID,
		CrawlRunSeed{},
		runCheckpointPreparation{},
		runSeedAdmissionResult{queued: 3, continued: true},
	)
	if seeded.RunID != runID || seeded.Queued != 3 {
		t.Fatalf("removed run seed result = %+v", seeded)
	}
}

func TestLoadBoundedSeedCandidatesPreservesStorageFailure(t *testing.T) {
	loadFailure := errors.New("seed storage read")
	checkpoint := &boundedCheckpointScript{seedPagesError: loadFailure}
	frontier := NewFrontier(2, nil, WithCheckpoint(checkpoint))
	_, _, _, err := frontier.loadBoundedSeedCandidates(
		t.Context(), []byte("seed-storage"), 3, 2,
	)
	if !errors.Is(err, loadFailure) {
		t.Fatalf("seed storage error = %v", err)
	}
}

func TestCompleteBoundedSeedAdmissionIgnoresRemovedRunState(t *testing.T) {
	frontier, runID, run := prepareBoundedSeedRun(t, &boundedCheckpointScript{})
	lockProducerRunDurability(run)
	run.recoveryLoading = true
	frontier.mu.Lock()
	delete(frontier.state.runs, runID)
	frontier.mu.Unlock()
	if !frontier.completeBoundedSeedAdmission(
		runID,
		run,
		boundedSeedAdmissionCompletion{
			next: 2, complete: true, candidateTotal: 1,
		},
	) {
		t.Fatal("removed admission reported storage failure")
	}
	if run.seedRecoveryCursor != 0 || run.seedFinishing {
		t.Fatalf(
			"removed admission changed cursor=%d finishing=%t",
			run.seedRecoveryCursor,
			run.seedFinishing,
		)
	}
}

func TestBoundedSeedBatchPreservesDuplicateTally(t *testing.T) {
	profile := internalProfile(t)
	page := boundedProducerPage(
		profile.Profile.Handle,
		"https://recovery.example/duplicate",
	)
	checkpoint := &boundedCheckpointScript{
		seedPages:           []frontiercheckpoint.Page{page},
		seedNext:            1,
		seedComplete:        true,
		seedBatchDuplicates: 1,
		admissionState: frontiercheckpoint.AdmissionBatchState{
			Visited: []bool{true},
		},
	}
	frontier, _, run := prepareBoundedSeedRun(t, checkpoint)
	if !frontier.refillBoundedSeed(t.Context()) {
		t.Fatal("duplicate seed batch did not advance")
	}
	if run.seedRecoveryCursor != 1 || !run.seedFinishing {
		t.Fatalf(
			"duplicate seed cursor=%d finishing=%t",
			run.seedRecoveryCursor,
			run.seedFinishing,
		)
	}
}
