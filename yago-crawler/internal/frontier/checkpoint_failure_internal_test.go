package frontier

import (
	"context"
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type scriptedCheckpoint struct {
	status              frontiercheckpoint.RunStatus
	statusError         error
	beginError          error
	admissionAdjustment int
	admissionError      error
	finishSeedingError  error
	completionError     error
	redirectDuplicate   bool
	redirectError       error
	hostProgressError   error
	snapshot            frontiercheckpoint.Snapshot
	loadError           error
	deleteError         error
	controlError        error
	cancelQueuedError   error
	statusCalls         int
	loadCalls           int
	deleteCalls         int
	admissionCalls      int
	hostProgressCalls   int
	controlUpdates      []frontiercheckpoint.ControlUpdate
	cancelQueuedCalls   int
}

func (checkpoint *scriptedCheckpoint) Inspect(
	context.Context,
	[]byte,
	[]byte,
) (frontiercheckpoint.RunState, error) {
	checkpoint.statusCalls++
	return frontiercheckpoint.RunState{
		Status:       checkpoint.status,
		Pages:        checkpoint.snapshot.Counters.Pages,
		Pending:      checkpoint.snapshot.Counters.Pending,
		Failed:       checkpoint.snapshot.Failed,
		Seeding:      checkpoint.snapshot.Seeding,
		SeedManifest: checkpoint.snapshot.SeedManifest,
		Control:      checkpoint.snapshot.Control,
		Tally:        checkpoint.snapshot.Tally,
	}, checkpoint.statusError
}

func (checkpoint *scriptedCheckpoint) Begin(
	context.Context,
	[]byte,
	[]byte,
	yagocrawlcontract.CrawlOrderPriority,
) error {
	return checkpoint.beginError
}

func (checkpoint *scriptedCheckpoint) BeginSeedManifest(
	_ context.Context,
	_ []byte,
	_ []byte,
	_ yagocrawlcontract.CrawlOrderPriority,
	pages []frontiercheckpoint.Page,
) error {
	if checkpoint.beginError == nil && checkpoint.snapshot.Seeding &&
		checkpoint.snapshot.SeedManifest && len(checkpoint.snapshot.SeedPages) == 0 {
		checkpoint.snapshot.SeedPages = append([]frontiercheckpoint.Page(nil), pages...)
	}
	return checkpoint.beginError
}

func (checkpoint *scriptedCheckpoint) Admit(
	_ context.Context,
	_ []byte,
	pages []frontiercheckpoint.Page,
) (int, error) {
	checkpoint.admissionCalls++
	return len(pages) + checkpoint.admissionAdjustment, checkpoint.admissionError
}

func (checkpoint *scriptedCheckpoint) AdmitSeedBatch(
	_ context.Context,
	_ []byte,
	batch frontiercheckpoint.SeedBatch,
) (frontiercheckpoint.SeedBatchResult, error) {
	checkpoint.admissionCalls++
	result := frontiercheckpoint.SeedBatchResult{}
	for _, decision := range batch.Decisions {
		if decision.Admit {
			result.Admitted++
		}
	}
	result.Admitted += checkpoint.admissionAdjustment
	return result, checkpoint.admissionError
}

func (checkpoint *scriptedCheckpoint) FinishSeeding(
	context.Context,
	[]byte,
	yagocrawlcontract.CrawlRunTally,
) error {
	return checkpoint.finishSeedingError
}

func (checkpoint *scriptedCheckpoint) CompletePage(
	_ context.Context,
	_ []byte,
	_ string,
	completion frontiercheckpoint.PageCompletion,
) error {
	if completion.HostProgress != nil {
		checkpoint.hostProgressCalls++
		if checkpoint.hostProgressError != nil {
			return checkpoint.hostProgressError
		}
	}
	return checkpoint.completionError
}

func (checkpoint *scriptedCheckpoint) RecordRedirect(
	context.Context,
	[]byte,
	frontiercheckpoint.Redirect,
) (bool, error) {
	return checkpoint.redirectDuplicate, checkpoint.redirectError
}

func (checkpoint *scriptedCheckpoint) RecordHostState(
	context.Context,
	[]byte,
	string,
	frontiercheckpoint.HostProgress,
	[]string,
) error {
	checkpoint.hostProgressCalls++
	return checkpoint.hostProgressError
}

func (checkpoint *scriptedCheckpoint) UpdateControl(
	_ context.Context,
	_ []byte,
	update frontiercheckpoint.ControlUpdate,
) error {
	checkpoint.controlUpdates = append(checkpoint.controlUpdates, update)
	return checkpoint.controlError
}

func (checkpoint *scriptedCheckpoint) CancelQueuedPages(
	context.Context,
	[]byte,
	[]string,
) error {
	checkpoint.cancelQueuedCalls++

	return checkpoint.cancelQueuedError
}

func (checkpoint *scriptedCheckpoint) Load(
	context.Context,
	[]byte,
) (frontiercheckpoint.Snapshot, error) {
	checkpoint.loadCalls++
	return checkpoint.snapshot, checkpoint.loadError
}

func (checkpoint *scriptedCheckpoint) Delete(context.Context, []byte) error {
	checkpoint.deleteCalls++
	return checkpoint.deleteError
}

func checkpointSnapshot(
	identity []byte,
	priority yagocrawlcontract.CrawlOrderPriority,
) frontiercheckpoint.Snapshot {
	return frontiercheckpoint.Snapshot{
		Visited:       make(map[string]struct{}),
		HostStates:    make(map[string]frontiercheckpoint.HostState),
		OrderIdentity: append([]byte(nil), identity...),
		Priority:      normalizeCrawlOrderPriority(priority),
		Seeding:       true,
		SeedManifest:  true,
	}
}

func checkpointSettlement(t *testing.T, settled <-chan bool) bool {
	t.Helper()
	select {
	case succeeded := <-settled:
		return succeeded
	case <-time.After(time.Second):
		t.Fatal("checkpointed run did not settle")
		return false
	}
}

func TestCheckpointFailureStopsDispatchAndPreservesFirstCause(t *testing.T) {
	var shutdowns atomic.Int32
	frontier := NewFrontier(1, nil, WithCheckpointFailureShutdown(func() {
		shutdowns.Add(1)
	}))
	frontier.RecordCheckpointFailure(nil)
	if failure := frontier.CheckpointFailure(); failure != nil {
		t.Fatalf("nil checkpoint failure = %v", failure)
	}
	first := errors.New("first checkpoint failure")
	frontier.RecordCheckpointFailure(first)
	var concurrent sync.WaitGroup
	for range 16 {
		concurrent.Add(1)
		go func() {
			defer concurrent.Done()
			frontier.RecordCheckpointFailure(errors.New("later checkpoint failure"))
		}()
	}
	concurrent.Wait()
	if !errors.Is(frontier.CheckpointFailure(), first) {
		t.Fatalf("checkpoint failure = %v, want first cause", frontier.CheckpointFailure())
	}
	if got := shutdowns.Load(); got != 1 {
		t.Fatalf("checkpoint shutdowns = %d, want 1", got)
	}
	if _, ok := frontier.Take(t.Context()); ok {
		t.Fatal("frontier dispatched after checkpoint failure")
	}
}

func TestRecoveryAndDeletionExposeCheckpointState(t *testing.T) {
	ctx := t.Context()
	identity := []byte("order-identity")
	provenance := []byte("order-provenance")
	withoutCheckpoint := NewFrontier(1, nil)
	if recovery, err := withoutCheckpoint.Recovery(ctx, provenance, identity); err != nil ||
		recovery != (RunRecovery{}) {
		t.Fatalf("recovery without checkpoint = %+v, %v", recovery, err)
	}
	if err := withoutCheckpoint.ForgetCheckpoint(ctx, provenance); err != nil {
		t.Fatalf("forget without checkpoint: %v", err)
	}

	checkpoint := &scriptedCheckpoint{}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	if recovery, err := frontier.Recovery(ctx, nil, identity); err != nil ||
		recovery != (RunRecovery{}) {
		t.Fatalf("empty provenance recovery = %+v, %v", recovery, err)
	}
	if err := frontier.ForgetCheckpoint(ctx, nil); err != nil {
		t.Fatalf("forget empty provenance: %v", err)
	}
	if checkpoint.statusCalls != 0 || checkpoint.deleteCalls != 0 {
		t.Fatalf("empty provenance touched checkpoint: %+v", checkpoint)
	}

	checkpoint.status = frontiercheckpoint.RunMissing
	if recovery, err := frontier.Recovery(ctx, provenance, identity); err != nil ||
		recovery != (RunRecovery{}) {
		t.Fatalf("missing recovery = %+v, %v", recovery, err)
	}
	if checkpoint.loadCalls != 0 {
		t.Fatalf("missing recovery loaded snapshot %d times", checkpoint.loadCalls)
	}

	checkpoint.status = frontiercheckpoint.RunActive
	checkpoint.snapshot = checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal)
	checkpoint.snapshot.Counters.Pages = 9
	checkpoint.snapshot.Counters.Pending = 7
	checkpoint.snapshot.Failed = true
	checkpoint.snapshot.Control.Cancelled = true
	recovery, err := frontier.Recovery(ctx, provenance, identity)
	if err != nil || recovery != (RunRecovery{
		Checkpointed: true,
		Seeding:      true,
		Pages:        9,
		Pending:      7,
		Failed:       true,
		Cancelled:    true,
		SeedManifest: true,
	}) {
		t.Fatalf("active recovery = %+v, %v", recovery, err)
	}
	checkpoint.status = frontiercheckpoint.RunCompleted
	checkpoint.snapshot.Seeding = false
	checkpoint.snapshot.SeedManifest = false
	recovery, err = frontier.Recovery(ctx, provenance, identity)
	if err != nil || recovery != (RunRecovery{
		Checkpointed: true,
		Completed:    true,
		Pages:        9,
		Pending:      7,
		Failed:       true,
		Cancelled:    true,
	}) {
		t.Fatalf("completed recovery = %+v, %v", recovery, err)
	}
	if err := frontier.ForgetCheckpoint(ctx, provenance); err != nil {
		t.Fatalf("forget checkpoint: %v", err)
	}
	if checkpoint.deleteCalls != 1 {
		t.Fatalf("checkpoint deletes = %d, want 1", checkpoint.deleteCalls)
	}
}

func TestRecoveryAndDeletionFailuresStopTheFrontier(t *testing.T) {
	tests := []struct {
		name       string
		checkpoint scriptedCheckpoint
		forget     bool
	}{
		{
			name:       "inspect",
			checkpoint: scriptedCheckpoint{statusError: errors.New("inspect failure")},
		},
		{
			name:       "delete",
			checkpoint: scriptedCheckpoint{deleteError: errors.New("delete failure")},
			forget:     true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			var shutdowns atomic.Int32
			checkpoint := testCase.checkpoint
			frontier := NewFrontier(
				1,
				nil,
				WithCheckpoint(&checkpoint),
				WithCheckpointFailureShutdown(func() { shutdowns.Add(1) }),
			)
			var err error
			if testCase.forget {
				err = frontier.ForgetCheckpoint(t.Context(), []byte("provenance"))
			} else {
				_, err = frontier.Recovery(
					t.Context(), []byte("provenance"), []byte("identity"),
				)
			}
			if err == nil || !errors.Is(err, frontier.CheckpointFailure()) {
				t.Fatalf(
					"operation error = %v, checkpoint failure = %v",
					err,
					frontier.CheckpointFailure(),
				)
			}
			if got := shutdowns.Load(); got != 1 {
				t.Fatalf("checkpoint shutdowns = %d, want 1", got)
			}
		})
	}
}

func TestSeedRunRejectsUnavailableOrCorruptCheckpoint(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("seed-identity")
	provenance := []byte("seed-provenance")
	loadFailure := errors.New("load checkpoint")
	beginFailure := errors.New("begin checkpoint")
	tests := []struct {
		name       string
		checkpoint scriptedCheckpoint
		target     error
	}{
		{
			name:       "begin",
			checkpoint: scriptedCheckpoint{beginError: beginFailure},
			target:     beginFailure,
		},
		{
			name:       "load",
			checkpoint: scriptedCheckpoint{loadError: loadFailure},
			target:     loadFailure,
		},
		{
			name: "snapshot",
			checkpoint: scriptedCheckpoint{snapshot: checkpointSnapshot(
				[]byte("different-identity"), yagocrawlcontract.CrawlOrderPriorityNormal,
			)},
			target: frontiercheckpoint.ErrCorruptCheckpoint,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := testCase.checkpoint
			settled := make(chan bool, 1)
			frontier := NewFrontier(1, nil, WithCheckpoint(&checkpoint))
			seeded := frontier.SeedRunWithPriority(
				t.Context(),
				CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
				profile,
				func(succeeded bool) { settled <- succeeded },
			)
			if seeded.Queued != 0 || checkpointSettlement(t, settled) {
				t.Fatalf("failed checkpoint seed = %+v", seeded)
			}
			if !errors.Is(frontier.CheckpointFailure(), testCase.target) {
				t.Fatalf(
					"checkpoint failure = %v, want %v",
					frontier.CheckpointFailure(),
					testCase.target,
				)
			}
		})
	}
}

func TestCompletedCheckpointSettlesWithoutRestoringWork(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("completed-identity")
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "succeeded", true: "failed"}[failed], func(t *testing.T) {
			snapshot := checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal)
			snapshot.Seeding = false
			snapshot.Completed = true
			snapshot.Failed = failed
			checkpoint := &scriptedCheckpoint{snapshot: snapshot}
			settled := make(chan bool, 1)
			frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
			seeded := frontier.SeedRunWithPriority(
				t.Context(),
				CrawlRunSeed{
					Requests:      internalRequests(profile, "https://example.com/completed"),
					Provenance:    []byte("completed-provenance"),
					OrderIdentity: identity,
				},
				profile,
				func(succeeded bool) { settled <- succeeded },
			)
			if seeded.Queued != 0 || checkpoint.admissionCalls != 0 {
				t.Fatalf(
					"completed checkpoint seed = %+v, admissions = %d",
					seeded,
					checkpoint.admissionCalls,
				)
			}
			if succeeded := checkpointSettlement(t, settled); succeeded == failed {
				t.Fatalf("completed checkpoint succeeded = %t, failed = %t", succeeded, failed)
			}
		})
	}
}

func TestRestoredProfileMismatchFailsTheRun(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("mismatch-identity")
	snapshot := checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal)
	snapshot.Seeding = false
	snapshot.Counters = frontiercheckpoint.Counters{Pages: 1, Pending: 1}
	snapshot.Visited["https://example.com/restored"] = struct{}{}
	snapshot.Outstanding = []frontiercheckpoint.Page{{
		URL:           "https://example.com/restored",
		Host:          "example.com",
		ProfileHandle: "different-profile",
		ObservationID: "restored-observation",
		ObservedAt:    time.Now().UTC(),
	}}
	checkpoint := &scriptedCheckpoint{snapshot: snapshot}
	settled := make(chan bool, 1)
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{Provenance: []byte("mismatch-provenance"), OrderIdentity: identity},
		profile,
		func(succeeded bool) { settled <- succeeded },
	)
	if seeded.Queued != 1 || checkpointSettlement(t, settled) {
		t.Fatalf("profile mismatch seed = %+v", seeded)
	}
	if !errors.Is(frontier.CheckpointFailure(), frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("profile mismatch failure = %v", frontier.CheckpointFailure())
	}
}

func TestSeedMutationFailureStopsBeforeDispatch(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("mutation-identity")
	provenance := []byte("mutation-provenance")
	admissionFailure := errors.New("admit checkpoint page")
	tests := []struct {
		name       string
		checkpoint scriptedCheckpoint
		target     error
	}{
		{
			name: "short admission",
			checkpoint: scriptedCheckpoint{
				admissionAdjustment: -1,
			},
			target: frontiercheckpoint.ErrCorruptCheckpoint,
		},
		{
			name:       "admission error",
			checkpoint: scriptedCheckpoint{admissionError: admissionFailure},
			target:     admissionFailure,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := testCase.checkpoint
			checkpoint.snapshot = checkpointSnapshot(
				identity, yagocrawlcontract.CrawlOrderPriorityNormal,
			)
			var shutdowns atomic.Int32
			frontier := NewFrontier(
				1,
				nil,
				WithCheckpoint(&checkpoint),
				WithCheckpointFailureShutdown(func() { shutdowns.Add(1) }),
			)
			seeded := frontier.SeedRunWithPriority(
				t.Context(),
				CrawlRunSeed{
					Requests:      internalRequests(profile, "https://example.com/page"),
					Provenance:    provenance,
					OrderIdentity: identity,
				},
				profile,
				nil,
			)
			if seeded.Queued != 1 || checkpoint.admissionCalls != 1 {
				t.Fatalf(
					"failed mutation seed = %+v, admissions = %d",
					seeded,
					checkpoint.admissionCalls,
				)
			}
			if !errors.Is(frontier.CheckpointFailure(), testCase.target) {
				t.Fatalf(
					"checkpoint failure = %v, want %v",
					frontier.CheckpointFailure(),
					testCase.target,
				)
			}
			if got := shutdowns.Load(); got != 1 {
				t.Fatalf("checkpoint shutdowns = %d, want 1", got)
			}
			if _, ok := frontier.Take(t.Context()); ok {
				t.Fatal("page dispatched after checkpoint mutation failure")
			}
		})
	}
}

func TestFinishSeedingFailureNacksAnEmptyRun(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("finish-identity")
	checkpointFailure := errors.New("finish checkpoint seeding")
	checkpoint := &scriptedCheckpoint{
		snapshot: checkpointSnapshot(
			identity,
			yagocrawlcontract.CrawlOrderPriorityNormal,
		),
		finishSeedingError: checkpointFailure,
	}
	settled := make(chan bool, 1)
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{Provenance: []byte("finish-provenance"), OrderIdentity: identity},
		profile,
		func(succeeded bool) { settled <- succeeded },
	)
	if checkpointSettlement(t, settled) {
		t.Fatal("run with failed seeding checkpoint succeeded")
	}
	if !errors.Is(frontier.CheckpointFailure(), checkpointFailure) {
		t.Fatalf("checkpoint failure = %v", frontier.CheckpointFailure())
	}
}

func TestPageCompletionFailureNacksTheRun(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("completion-identity")
	completionFailure := errors.New("complete checkpoint page")
	checkpoint := &scriptedCheckpoint{
		snapshot:        checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
		completionError: completionFailure,
	}
	settled := make(chan bool, 1)
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://example.com/completion"),
			Provenance:    []byte("completion-provenance"),
			OrderIdentity: identity,
		},
		profile,
		func(succeeded bool) { settled <- succeeded },
	)
	work := internalReceive(t, frontier)
	frontier.Done(work, successfulPageOutcome())
	if checkpointSettlement(t, settled) {
		t.Fatal("run with failed page checkpoint succeeded")
	}
	if !errors.Is(frontier.CheckpointFailure(), completionFailure) {
		t.Fatalf("completion checkpoint failure = %v", frontier.CheckpointFailure())
	}
}

func TestCheckpointSnapshotValidationRejectsUnsafeRecovery(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("validation-identity")
	seed := CrawlRunSeed{
		Provenance:    []byte("validation-provenance"),
		OrderIdentity: identity,
		Priority:      yagocrawlcontract.CrawlOrderPriorityNormal,
	}
	validPage := frontiercheckpoint.Page{
		URL:           "https://example.com/page",
		Host:          "example.com",
		Depth:         1,
		ProfileHandle: profile.Profile.Handle,
		ObservationID: "observation",
		ObservedAt:    time.Now().UTC(),
	}
	tests := unsafeCheckpointSnapshotMutations(validPage)
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot := checkpointSnapshot(identity, seed.Priority)
			testCase.mutate(&snapshot)
			if err := validateCheckpointSnapshot(
				snapshot,
				seed,
				normalizeCrawlOrderPriority(seed.Priority),
				profile.Profile.Handle,
			); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
				t.Fatalf("snapshot validation error = %v", err)
			}
		})
	}
}

type checkpointSnapshotMutation struct {
	name   string
	mutate func(*frontiercheckpoint.Snapshot)
}

func unsafeCheckpointSnapshotMutations(
	validPage frontiercheckpoint.Page,
) []checkpointSnapshotMutation {
	mutations := make([]checkpointSnapshotMutation, 0, 12)
	mutations = append(
		mutations,
		checkpointSnapshotMutation{
			name: "identity",
			mutate: func(snapshot *frontiercheckpoint.Snapshot) {
				snapshot.OrderIdentity = []byte("different")
			},
		},
		checkpointSnapshotMutation{
			name: "priority",
			mutate: func(snapshot *frontiercheckpoint.Snapshot) {
				snapshot.Priority = yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery
			},
		},
		checkpointSnapshotMutation{
			name: "run pages",
			mutate: func(snapshot *frontiercheckpoint.Snapshot) {
				snapshot.Counters.Pages = uint64(math.MaxInt) + 1
			},
		},
		checkpointSnapshotMutation{
			name: "budget discarded pages",
			mutate: func(snapshot *frontiercheckpoint.Snapshot) {
				snapshot.BudgetDiscardedPages = 1
			},
		},
		checkpointSnapshotMutation{
			name: "empty host",
			mutate: func(snapshot *frontiercheckpoint.Snapshot) {
				snapshot.HostStates[""] = frontiercheckpoint.HostState{}
			},
		},
		checkpointSnapshotMutation{
			name: "host pages",
			mutate: func(snapshot *frontiercheckpoint.Snapshot) {
				snapshot.HostStates["example.com"] = frontiercheckpoint.HostState{
					Pages: uint64(math.MaxInt) + 1,
				}
			},
		},
	)

	return append(mutations, unsafeCheckpointPageMutations(validPage)...)
}

func unsafeCheckpointPageMutations(validPage frontiercheckpoint.Page) []checkpointSnapshotMutation {
	return []checkpointSnapshotMutation{
		{name: "page url", mutate: func(snapshot *frontiercheckpoint.Snapshot) {
			page := validPage
			page.URL = "://invalid"
			snapshot.Outstanding = []frontiercheckpoint.Page{page}
		}},
		{name: "normalized page", mutate: func(snapshot *frontiercheckpoint.Snapshot) {
			page := validPage
			page.URL = "HTTPS://EXAMPLE.COM/page"
			snapshot.Outstanding = []frontiercheckpoint.Page{page}
		}},
		{name: "page host", mutate: func(snapshot *frontiercheckpoint.Snapshot) {
			page := validPage
			page.Host = "other.example"
			snapshot.Outstanding = []frontiercheckpoint.Page{page}
		}},
		{name: "profile", mutate: func(snapshot *frontiercheckpoint.Snapshot) {
			page := validPage
			page.ProfileHandle = ""
			snapshot.Outstanding = []frontiercheckpoint.Page{page}
		}},
		{name: "observation", mutate: func(snapshot *frontiercheckpoint.Snapshot) {
			page := validPage
			page.ObservationID = ""
			snapshot.Outstanding = []frontiercheckpoint.Page{page}
		}},
		{name: "observation time", mutate: func(snapshot *frontiercheckpoint.Snapshot) {
			page := validPage
			page.ObservedAt = time.Time{}
			snapshot.Outstanding = []frontiercheckpoint.Page{page}
		}},
		{name: "seed manifest profile", mutate: func(snapshot *frontiercheckpoint.Snapshot) {
			page := validPage
			page.ProfileHandle = "different-profile"
			snapshot.SeedPages = []frontiercheckpoint.Page{page}
		}},
	}
}

func TestCheckpointRestorePreservesFailureAndHostState(t *testing.T) {
	profile := internalProfile(t)
	frontier := NewFrontier(2, nil)
	runID := uuid.New()
	settled := func(bool) {}
	frontier.state.beginRun(runID, []byte("restore-provenance"), profile, settled)
	observedAt := time.Now().UTC()
	snapshot := frontiercheckpoint.Snapshot{
		Visited: map[string]struct{}{
			"https://retired.example/visited": {},
		},
		Counters: frontiercheckpoint.Counters{Pages: 3, Pending: 1},
		HostStates: map[string]frontiercheckpoint.HostState{
			"retired.example": {Pages: 2, Failures: 4, Retired: true},
			"active.example":  {Pages: 1},
		},
		Outstanding: []frontiercheckpoint.Page{{
			URL:           "https://active.example/pending",
			Host:          "active.example",
			Depth:         1,
			ProfileHandle: profile.Profile.Handle,
			ObservationID: "restored-observation",
			ObservedAt:    observedAt,
			Index:         true,
		}},
		Priority: yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		Failed:   true,
	}
	if err := frontier.restoreCheckpointRunLocked(runID, snapshot, profile); err != nil {
		t.Fatalf("restore checkpoint run: %v", err)
	}
	run := frontier.state.runs[runID]
	if run.priority != yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery || run.pages != 3 ||
		run.hostPages["retired.example"] != 2 || run.hostFailures["retired.example"] != 4 {
		t.Fatalf("restored run counters = %+v", run)
	}
	if _, retired := run.retiredHosts["retired.example"]; !retired {
		t.Fatal("retired host was not restored")
	}
	if _, visited := run.visited["https://retired.example/visited"]; !visited {
		t.Fatal("visited URL was not restored")
	}
	if run.pendingPages != 1 || frontier.state.completion.Pending(runID) != 2 {
		t.Fatalf(
			"restored pending = %d, completion = %d",
			run.pendingPages,
			frontier.state.completion.Pending(runID),
		)
	}
	finish, succeeded, drained := frontier.state.completion.SettleMany(runID, 2)
	if finish == nil || !succeeded || !drained {
		t.Fatalf("failed restore settlement = %t, %t, %t", finish != nil, succeeded, drained)
	}

	mismatchID := uuid.New()
	frontier.state.beginRun(mismatchID, []byte("profile-mismatch"), profile, nil)
	mismatch := snapshot
	mismatch.Outstanding = append([]frontiercheckpoint.Page(nil), snapshot.Outstanding...)
	mismatch.Outstanding[0].ProfileHandle = "different-profile"
	if err := frontier.restoreCheckpointRunLocked(
		mismatchID, mismatch, profile,
	); !errors.Is(err, frontiercheckpoint.ErrCorruptCheckpoint) {
		t.Fatalf("profile mismatch error = %v", err)
	}
}

func TestHostCheckpointFailureStopsFurtherProgressWrites(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("host-identity")
	hostFailure := errors.New("persist host progress")
	checkpoint := &scriptedCheckpoint{
		snapshot:          checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
		hostProgressError: hostFailure,
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://failed.example/page"),
			Provenance:    []byte("host-provenance"),
			OrderIdentity: identity,
		},
		profile,
		nil,
	)
	work := internalReceive(t, frontier)
	frontier.RecordHostFetchOutcome(t.Context(), work, true)
	if frontier.CheckpointFailure() != nil || checkpoint.hostProgressCalls != 0 {
		t.Fatalf(
			"host progress committed before page completion: %v, %d",
			frontier.CheckpointFailure(),
			checkpoint.hostProgressCalls,
		)
	}
	frontier.Done(work, successfulPageOutcome())
	if !errors.Is(frontier.CheckpointFailure(), hostFailure) || checkpoint.hostProgressCalls != 1 {
		t.Fatalf(
			"host checkpoint failure = %v, writes = %d",
			frontier.CheckpointFailure(),
			checkpoint.hostProgressCalls,
		)
	}
	frontier.RecordHostFetchOutcome(t.Context(), work, true)
	if checkpoint.hostProgressCalls != 1 {
		t.Fatalf("host progress writes after failure = %d", checkpoint.hostProgressCalls)
	}
	if frontier.RunPending(seeded.RunID) != 0 {
		t.Fatalf("pending after host checkpoint failure = %d", frontier.RunPending(seeded.RunID))
	}
}

func TestQueuedHostURLsIncludeEveryDroppableQueue(t *testing.T) {
	frontier := NewFrontier(4, nil)
	unknownRun := uuid.New()
	frontier.state.ready = []crawljob.CrawlJob{
		{RunID: unknownRun, URL: "https://example.com/ready"},
		{RunID: unknownRun, URL: "https://other.example/ready"},
	}
	if got := frontier.queuedHostURLsLocked(unknownRun, "example.com"); len(got) != 1 ||
		got[0] != "https://example.com/ready" {
		t.Fatalf("unknown run queued URLs = %v", got)
	}

	profile := internalProfile(t)
	knownRun := uuid.New()
	frontier.state.beginRun(knownRun, []byte("known-run"), profile, nil)
	if got := frontier.queuedHostURLsLocked(knownRun, "missing.example"); len(got) != 0 {
		t.Fatalf("missing host queued URLs = %v", got)
	}
	bucket := frontier.state.runs[knownRun].pendingHost("example.com")
	bucket.returned = []pendingPage{
		{normURL: "https://example.com/already-returned"},
		{normURL: "https://example.com/returned"},
	}
	bucket.returnedHead = 1
	bucket.queued = []pendingPage{
		{normURL: "https://example.com/already-queued"},
		{normURL: "https://example.com/queued"},
	}
	bucket.queuedHead = 1
	frontier.state.ready = append(frontier.state.ready, crawljob.CrawlJob{
		RunID: knownRun,
		URL:   "https://example.com/known-ready",
	})
	got := frontier.queuedHostURLsLocked(knownRun, "example.com")
	want := []string{
		"https://example.com/known-ready",
		"https://example.com/returned",
		"https://example.com/queued",
	}
	if len(got) != len(want) {
		t.Fatalf("queued host URLs = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("queued host URLs = %v, want %v", got, want)
		}
	}
}
