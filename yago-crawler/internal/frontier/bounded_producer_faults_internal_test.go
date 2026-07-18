package frontier

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type boundedCheckpointScript struct {
	scriptedCheckpoint
	boundedSnapshot       frontiercheckpoint.Snapshot
	boundedSnapshotError  error
	recoveryBatch         frontiercheckpoint.RecoveryPageBatch
	recoveryBatchError    error
	seedPages             []frontiercheckpoint.Page
	seedNext              uint64
	seedComplete          bool
	seedPagesError        error
	admissionState        frontiercheckpoint.AdmissionBatchState
	admissionStateError   error
	finishSeedingDone     bool
	finishSeedingBatchErr error
	cancelSeedDone        bool
	cancelSeedError       error
	seedBatchDuplicates   uint64
	onRecoveryLoad        func()
	onSeedLoad            func()
	onAdmissionLoad       func()
}

func (checkpoint *boundedCheckpointScript) AdmitSeedBatch(
	_ context.Context,
	_ []byte,
	batch frontiercheckpoint.SeedBatch,
) (frontiercheckpoint.SeedBatchResult, error) {
	checkpoint.admissionCalls++
	result := frontiercheckpoint.SeedBatchResult{
		Duplicates: checkpoint.seedBatchDuplicates,
	}
	for _, decision := range batch.Decisions {
		if decision.Admit {
			result.Admitted++
		}
	}
	result.Admitted += checkpoint.admissionAdjustment

	return result, checkpoint.admissionError
}

func (checkpoint *boundedCheckpointScript) LoadBounded(
	context.Context,
	[]byte,
	int,
) (frontiercheckpoint.Snapshot, error) {
	return checkpoint.boundedSnapshot, checkpoint.boundedSnapshotError
}

func (checkpoint *boundedCheckpointScript) LoadRecoveryPageBatch(
	context.Context,
	[]byte,
	uint64,
	uint64,
	int,
) (frontiercheckpoint.RecoveryPageBatch, error) {
	if checkpoint.onRecoveryLoad != nil {
		checkpoint.onRecoveryLoad()
	}
	return checkpoint.recoveryBatch, checkpoint.recoveryBatchError
}

func (checkpoint *boundedCheckpointScript) LoadSeedPageBatch(
	context.Context,
	[]byte,
	uint64,
	int,
) ([]frontiercheckpoint.Page, uint64, bool, error) {
	if checkpoint.onSeedLoad != nil {
		checkpoint.onSeedLoad()
	}
	return checkpoint.seedPages, checkpoint.seedNext, checkpoint.seedComplete,
		checkpoint.seedPagesError
}

func (checkpoint *boundedCheckpointScript) AdmissionBatchState(
	_ context.Context,
	_ []byte,
	pages []frontiercheckpoint.Page,
) (frontiercheckpoint.AdmissionBatchState, error) {
	if checkpoint.onAdmissionLoad != nil {
		checkpoint.onAdmissionLoad()
	}
	state := checkpoint.admissionState
	if state.Visited != nil && state.HostStates == nil {
		state.HostStates = make(map[string]frontiercheckpoint.HostState)
		for _, page := range pages {
			state.HostStates[page.Host] = frontiercheckpoint.HostState{}
		}
	}

	return state, checkpoint.admissionStateError
}

func (*boundedCheckpointScript) CancelRecoveryPages(
	context.Context,
	[]byte,
	uint64,
	uint64,
) (uint64, error) {
	return 0, nil
}

func (checkpoint *boundedCheckpointScript) FinishSeedingBatch(
	context.Context,
	[]byte,
	yagocrawlcontract.CrawlRunTally,
) (bool, error) {
	return checkpoint.finishSeedingDone, checkpoint.finishSeedingBatchErr
}

func (checkpoint *boundedCheckpointScript) CancelSeedManifestBatch(
	context.Context,
	[]byte,
) (bool, error) {
	return checkpoint.cancelSeedDone, checkpoint.cancelSeedError
}

type recoveryVisitPace struct {
	visited []crawljob.CrawlJob
}

func (*recoveryVisitPace) DueAt(_ crawljob.CrawlJob, now time.Time) time.Time {
	return now
}

func (pace *recoveryVisitPace) Visited(job crawljob.CrawlJob, _ time.Time) {
	pace.visited = append(pace.visited, job)
}

func boundedProducerPage(profileHandle string, rawURL string) frontiercheckpoint.Page {
	return frontiercheckpoint.Page{
		URL:           rawURL,
		Host:          "recovery.example",
		ProfileHandle: profileHandle,
		ObservationID: "bounded-observation",
		ObservedAt:    time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
		Index:         true,
	}
}

func beginBoundedProducerRun(
	t *testing.T,
	checkpoint Checkpoint,
) (*Frontier, uuid.UUID, *crawlRun, crawladmission.AdmissionProfile) {
	t.Helper()
	profile := internalProfile(t)
	provenance := []byte("bounded-producer-run")
	frontier := NewFrontier(8, nil, WithCheckpoint(checkpoint))
	runID := uuid.New()
	frontier.mu.Lock()
	frontier.state.beginRun(runID, provenance, profile, func(bool) {})
	run := frontier.state.runs[runID]
	run.seeding = false
	run.boundedRecovery = true
	run.recoveryUpper = 10
	frontier.mu.Unlock()

	return frontier, runID, run, profile
}

func lockProducerRunDurability(run *crawlRun) {
	run.durability.Lock()
	run.awaitingDurability = true
}

func TestBoundedAdmissionStateRejectsReadFailureAndInvalidShape(t *testing.T) {
	readFailure := errors.New("read admission state")
	checkpoint := &boundedCheckpointScript{admissionStateError: readFailure}
	frontier, _, run, profile := beginBoundedProducerRun(t, checkpoint)
	candidate := checkpointCandidate(
		boundedProducerPage(profile.Profile.Handle, "https://recovery.example/page"),
		run.provenanceValue,
	)
	if _, err := frontier.loadBoundedAdmissionState(
		t.Context(), run, []frontierCandidate{candidate},
	); !errors.Is(err, readFailure) {
		t.Fatalf("admission read error = %v", err)
	}
	if _, err := newBoundedAdmissionWindow(
		run, frontiercheckpoint.AdmissionBatchState{}, []frontierCandidate{candidate},
	); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("admission shape error = %v", err)
	}
	if _, err := newBoundedAdmissionWindow(run, frontiercheckpoint.AdmissionBatchState{
		Visited: []bool{false},
		HostStates: map[string]frontiercheckpoint.HostState{
			"": {},
		},
	}, []frontierCandidate{candidate}); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("admission host error = %v", err)
	}
	missingProfile := candidate
	missingProfile.profileHandle = "missing"
	if _, err := newBoundedAdmissionWindow(
		run,
		frontiercheckpoint.AdmissionBatchState{
			Visited: []bool{false},
			HostStates: map[string]frontiercheckpoint.HostState{
				candidate.host: {},
			},
		},
		[]frontierCandidate{missingProfile},
	); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("admission profile error = %v", err)
	}
}

func TestBoundedRecoveryGrowthTracksCommittedSequenceAndRejectsOverflow(t *testing.T) {
	run := &crawlRun{
		boundedRecovery:  true,
		recoveryCursor:   5,
		recoveryUpper:    5,
		recoveryComplete: true,
	}
	if err := extendBoundedRecovery(run, 2); err != nil {
		t.Fatalf("extend committed sequence: %v", err)
	}
	if run.recoveryUpper != 7 || run.recoveryComplete {
		t.Fatalf(
			"extended recovery boundary = %d complete=%t",
			run.recoveryUpper,
			run.recoveryComplete,
		)
	}
	run.recoveryUpper = math.MaxUint64
	if err := extendBoundedRecovery(run, 1); !errors.Is(
		err,
		frontiercheckpoint.ErrCorruptCheckpoint,
	) {
		t.Fatalf("overflow recovery boundary error = %v", err)
	}
	if run.recoveryUpper != math.MaxUint64 {
		t.Fatalf("overflow changed recovery boundary = %d", run.recoveryUpper)
	}
}

func TestBoundedRecoveryBatchValidationRejectsBrokenWindows(t *testing.T) {
	tests := []struct {
		name  string
		after uint64
		upper uint64
		limit int
		batch frontiercheckpoint.RecoveryPageBatch
	}{
		{
			name:  "retired overflow",
			upper: 300,
			limit: frontiercheckpoint.RecoveryPageBatchSize,
			batch: frontiercheckpoint.RecoveryPageBatch{
				Cursor: 300, Complete: true,
				RetiredPages: frontiercheckpoint.RecoveryPageBatchSize + 1,
			},
		},
		{
			name: "cursor reversal", after: 2, upper: 4, limit: 2,
			batch: frontiercheckpoint.RecoveryPageBatch{Cursor: 1},
		},
		{
			name: "stalled cursor", after: 2, upper: 4, limit: 2,
			batch: frontiercheckpoint.RecoveryPageBatch{Cursor: 2},
		},
		{
			name: "combined overflow", upper: 4, limit: 2,
			batch: frontiercheckpoint.RecoveryPageBatch{
				Cursor: 2, Pages: make([]frontiercheckpoint.Page, 2), RetiredPages: 1,
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateBoundedRecoveryBatch(
				testCase.after, testCase.upper, testCase.limit, testCase.batch,
			); err == nil {
				t.Fatal("invalid recovery window was accepted")
			}
		})
	}
}

func TestBoundedRecoveryAppendValidatesAndRestoresDurableState(t *testing.T) {
	checkpoint := &boundedCheckpointScript{}
	pace := &recoveryVisitPace{}
	frontier, runID, run, profile := beginBoundedProducerRun(t, checkpoint)
	frontier.pace = pace
	if err := frontier.appendBoundedRecoveryBatchLocked(runID, run,
		frontiercheckpoint.RecoveryPageBatch{HostStates: map[string]frontiercheckpoint.HostState{
			"": {},
		}},
	); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("invalid recovered host error = %v", err)
	}
	missingProfile := boundedProducerPage("missing-profile", "https://recovery.example/missing")
	if err := frontier.appendBoundedRecoveryBatchLocked(runID, run,
		frontiercheckpoint.RecoveryPageBatch{Pages: []frontiercheckpoint.Page{missingProfile}},
	); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("missing recovered profile error = %v", err)
	}
	invalidPage := boundedProducerPage(profile.Profile.Handle, "://invalid")
	if err := frontier.appendBoundedRecoveryBatchLocked(runID, run,
		frontiercheckpoint.RecoveryPageBatch{Pages: []frontiercheckpoint.Page{invalidPage}},
	); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("invalid recovered page error = %v", err)
	}
	page := boundedProducerPage(profile.Profile.Handle, "https://recovery.example/source")
	page.RedirectURL = "https://redirect.example/final"
	page.RedirectHost = "redirect.example"
	page.RedirectHostBump = true
	if err := frontier.appendBoundedRecoveryBatchLocked(runID, run,
		frontiercheckpoint.RecoveryPageBatch{
			Pages: []frontiercheckpoint.Page{page},
			HostStates: map[string]frontiercheckpoint.HostState{
				"recovery.example": {Pages: 1, Generation: 3},
			},
		},
	); err != nil {
		t.Fatalf("append recovered page: %v", err)
	}
	if run.pendingPages != 1 || len(pace.visited) != 1 ||
		run.redirects[page.URL].URL != page.RedirectURL {
		t.Fatalf(
			"restored pending=%d visits=%d redirect=%+v",
			run.pendingPages,
			len(pace.visited),
			run.redirects[page.URL],
		)
	}
}

func TestBoundedRecoveryRefillFailsClosedWithoutDurabilityOrStorage(t *testing.T) {
	profile := internalProfile(t)
	withoutCheckpoint := NewFrontier(8, nil)
	runID := uuid.New()
	withoutCheckpoint.mu.Lock()
	withoutCheckpoint.state.beginRun(runID, []byte("volatile-bounded"), profile, nil)
	run := withoutCheckpoint.state.runs[runID]
	run.seeding = false
	run.boundedRecovery = true
	run.recoveryUpper = 1
	withoutCheckpoint.mu.Unlock()
	if withoutCheckpoint.refillBoundedRecovery(t.Context()) {
		t.Fatal("volatile bounded run reported recovered work")
	}
	if run.recoveryLoading {
		t.Fatal("volatile bounded run remained loading")
	}

	loadFailure := errors.New("load recovery page batch")
	checkpoint := &boundedCheckpointScript{recoveryBatchError: loadFailure}
	frontier, _, recovered, _ := beginBoundedProducerRun(t, checkpoint)
	if frontier.refillBoundedRecovery(t.Context()) {
		t.Fatal("failed bounded recovery reported work")
	}
	if !errors.Is(frontier.CheckpointFailure(), loadFailure) || recovered.recoveryLoading {
		t.Fatalf(
			"recovery failure=%v loading=%t",
			frontier.CheckpointFailure(),
			recovered.recoveryLoading,
		)
	}
	frontier.finishBoundedRecoveryFailure(uuid.New(), nil, loadFailure)
}

func TestBoundedRecoveryLoadRejectsDisappearedRunAndFullWindow(t *testing.T) {
	checkpoint := &boundedCheckpointScript{}
	frontier, _, _, _ := beginBoundedProducerRun(t, checkpoint)
	if _, ready := frontier.beginBoundedRecoveryLoad(uuid.New()); ready {
		t.Fatal("missing run started recovery")
	}

	staleFrontier, staleID, staleRun, _ := beginBoundedProducerRun(t, checkpoint)
	staleFrontier.mu.Lock()
	staleRun.boundedRecovery = false
	staleRun.recoveryLoading = true
	staleFrontier.mu.Unlock()
	if _, ready := staleFrontier.beginBoundedRecoveryLoad(staleID); ready {
		t.Fatal("stale run started recovery")
	}

	fullFrontier, fullID, fullRun, _ := beginBoundedProducerRun(t, checkpoint)
	fullFrontier.mu.Lock()
	fullRun.pendingPages = frontiercheckpoint.RecoveryPageBatchSize
	fullRun.recoveryLoading = true
	fullFrontier.mu.Unlock()
	if _, ready := fullFrontier.beginBoundedRecoveryLoad(fullID); ready {
		t.Fatal("full live window started recovery")
	}
}

func TestBoundedRecoveryLoadAndApplyFenceConcurrentStateChanges(t *testing.T) {
	checkpoint := &boundedCheckpointScript{recoveryBatch: frontiercheckpoint.RecoveryPageBatch{
		Cursor: 1,
	}}
	frontier, runID, run, _ := beginBoundedProducerRun(t, checkpoint)
	if _, err := frontier.loadBoundedRecoveryBatch(t.Context(), boundedRecoveryLoad{
		runID: runID, run: run, cursor: 1, upper: 10, limit: 2,
	}); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("invalid recovery batch error = %v", err)
	}

	staleFrontier, staleID, staleRun, _ := beginBoundedProducerRun(t, checkpoint)
	lockProducerRunDurability(staleRun)
	staleRun.recoveryLoading = true
	staleFrontier.mu.Lock()
	delete(staleFrontier.state.runs, staleID)
	staleFrontier.mu.Unlock()
	if staleFrontier.applyBoundedRecoveryBatch(boundedRecoveryLoad{
		runID: staleID, run: staleRun,
	}, frontiercheckpoint.RecoveryPageBatch{}) {
		t.Fatal("disappeared run accepted a recovery batch")
	}

	invalidFrontier, invalidID, invalidRun, _ := beginBoundedProducerRun(t, checkpoint)
	lockProducerRunDurability(invalidRun)
	invalidRun.recoveryLoading = true
	if invalidFrontier.applyBoundedRecoveryBatch(boundedRecoveryLoad{
		runID: invalidID, run: invalidRun,
	}, frontiercheckpoint.RecoveryPageBatch{Pages: []frontiercheckpoint.Page{{
		URL: "https://recovery.example/invalid", Host: "recovery.example",
		ProfileHandle: "missing", ObservationID: "missing-profile", ObservedAt: time.Now(),
	}}}) {
		t.Fatal("invalid recovery page was accepted")
	}
	if !errors.Is(invalidFrontier.CheckpointFailure(), frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("invalid apply failure = %v", invalidFrontier.CheckpointFailure())
	}
}

func TestCheckpointRecoveryRejectsCapabilityBoundsAndRedirectCorruption(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("legacy-bounded-identity")
	legacy := &scriptedCheckpoint{snapshot: checkpointSnapshot(
		identity, yagocrawlcontract.CrawlOrderPriorityNormal,
	)}
	legacy.snapshot.RecoveryBounded = true
	frontier := NewFrontier(2, nil, WithCheckpoint(legacy))
	_, _, err := frontier.loadCheckpointRun(
		t.Context(),
		CrawlRunSeed{Provenance: []byte("legacy-bounded"), OrderIdentity: identity},
		nil,
		profile.Profile.Handle,
		frontiercheckpoint.RunState{Status: frontiercheckpoint.RunActive},
	)
	if !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("legacy bounded recovery error = %v", err)
	}

	seed := CrawlRunSeed{
		Provenance: []byte("validation"), OrderIdentity: identity,
		Priority: yagocrawlcontract.CrawlOrderPriorityNormal,
	}
	tests := []frontiercheckpoint.Snapshot{
		{
			OrderIdentity: identity, Priority: seed.Priority, RecoveryBounded: true,
			RecoveryCursor: 2, RecoveryUpper: 1,
		},
		{
			OrderIdentity: identity, Priority: seed.Priority,
			Outstanding: []frontiercheckpoint.Page{{
				URL: "https://recovery.example/page", Host: "recovery.example",
				ProfileHandle: profile.Profile.Handle, ObservationID: "redirect-state",
				ObservedAt: time.Now(), RedirectHost: "unexpected.example",
			}},
		},
		{
			OrderIdentity: identity, Priority: seed.Priority,
			Outstanding: []frontiercheckpoint.Page{{
				URL: "https://recovery.example/page", Host: "recovery.example",
				ProfileHandle: profile.Profile.Handle, ObservationID: "redirect-url",
				ObservedAt: time.Now(), RedirectURL: "://invalid",
			}},
		},
	}
	for index, snapshot := range tests {
		if err := validateCheckpointSnapshot(
			snapshot, seed, seed.Priority, profile.Profile.Handle,
		); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
			t.Fatalf("invalid checkpoint %d error = %v", index, err)
		}
	}
}

func TestCheckpointRestoreRejectsOverflowAndPreservesKnownHosts(t *testing.T) {
	profile := internalProfile(t)
	frontier := NewFrontier(2, nil)
	overflowID := uuid.New()
	frontier.state.beginRun(overflowID, []byte("overflow"), profile, nil)
	if err := frontier.restoreCheckpointRunLocked(
		overflowID,
		frontiercheckpoint.Snapshot{
			Counters:        frontiercheckpoint.Counters{Pending: uint64(math.MaxInt)},
			RecoveryBounded: true,
		},
		profile,
	); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("pending overflow error = %v", err)
	}

	runID := uuid.New()
	frontier.state.beginRun(runID, []byte("host-state"), profile, nil)
	run := frontier.state.runs[runID]
	run.hostPages["known.example"] = 7
	if err := restoreCheckpointHostStates(run, map[string]frontiercheckpoint.HostState{
		"known.example": {Pages: 2, Failures: 3, Retired: true},
	}); err != nil {
		t.Fatalf("restore known host: %v", err)
	}
	if run.hostPages["known.example"] != 7 || run.hostFailures["known.example"] != 0 {
		t.Fatalf("known host was overwritten: %+v", run)
	}
	if err := restoreCheckpointHostStates(run, map[string]frontiercheckpoint.HostState{
		"": {},
	}); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("invalid restore host error = %v", err)
	}
}

func TestPersistAcceptedPropagatesStorageOutcomes(t *testing.T) {
	checkpoint := &scriptedCheckpoint{}
	frontier := NewFrontier(2, nil, WithCheckpoint(checkpoint))
	run := &crawlRun{provenanceValue: []byte("persist-accepted")}
	candidate := frontierCandidate{
		normURL: "https://accepted.example/page", host: "accepted.example",
		profileHandle: "profile", observationID: "accepted", observedAt: time.Now(),
	}
	if err := frontier.persistAccepted(t.Context(), run, nil); err != nil {
		t.Fatalf("persist empty candidates: %v", err)
	}
	writeFailure := errors.New("write accepted page")
	checkpoint.admissionError = writeFailure
	if err := frontier.persistAccepted(
		t.Context(), run, []frontierCandidate{candidate},
	); !errors.Is(err, writeFailure) {
		t.Fatalf("persist write error = %v", err)
	}
	checkpoint.admissionError = nil
	checkpoint.admissionAdjustment = -1
	if err := frontier.persistAccepted(
		t.Context(), run, []frontierCandidate{candidate},
	); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("persist short write error = %v", err)
	}
	checkpoint.admissionAdjustment = 0
	if err := frontier.persistAccepted(
		t.Context(), run, []frontierCandidate{candidate},
	); err != nil {
		t.Fatalf("persist accepted page: %v", err)
	}
}
