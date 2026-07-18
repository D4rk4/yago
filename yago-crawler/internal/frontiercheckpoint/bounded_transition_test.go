package frontiercheckpoint

import (
	"context"
	"errors"
	"testing"
)

func TestFinishSeedingBatchPersistsCleanupAcrossBoundedCalls(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("bounded-finish")
	pages := largeSeedManifestPages(seedManifestRowsPerTransaction*2 + 1)
	beginSeedManifest(t, checkpoint, provenance, pages)
	for cursor := 0; cursor < len(pages); cursor += SeedAdmissionBatchSize {
		end := min(cursor+SeedAdmissionBatchSize, len(pages))
		decisions := make([]SeedDecision, end-cursor)
		for index := range decisions {
			decisions[index] = SeedDecision{Page: pages[cursor+index]}
		}
		if _, err := checkpoint.AdmitSeedBatch(testContext, provenance, SeedBatch{
			Cursor: uint64(cursor), Decisions: decisions,
		}); err != nil {
			t.Fatalf("advance manifest cursor %d: %v", cursor, err)
		}
	}

	done, err := checkpoint.FinishSeedingBatch(testContext, provenance, testRunTally())
	if err != nil || done {
		t.Fatalf("first bounded finish = %v, %v", done, err)
	}
	done, err = checkpoint.FinishSeedingBatch(testContext, provenance, testRunTally())
	if err != nil || done {
		t.Fatalf("second bounded finish = %v, %v", done, err)
	}
	done, err = checkpoint.FinishSeedingBatch(testContext, provenance, testRunTally())
	if err != nil || !done {
		t.Fatalf("final bounded finish = %v, %v", done, err)
	}
	done, err = checkpoint.FinishSeedingBatch(testContext, provenance, testRunTally())
	if err != nil || !done {
		t.Fatalf("replayed bounded finish = %v, %v", done, err)
	}
}

func TestFinishSeedingBatchRejectsIncompleteAndInvalidRuns(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	page := testPage("https://batch.example/page", "batch.example", "batch", 0)
	provenance := []byte("bounded-incomplete")
	beginSeedManifest(t, checkpoint, provenance, []Page{page})
	if done, err := checkpoint.FinishSeedingBatch(
		testContext, provenance, testRunTally(),
	); done || !errors.Is(err, ErrInvalidSeedBatch) {
		t.Fatalf("incomplete bounded finish = %v, %v", done, err)
	}
	if done, err := checkpoint.FinishSeedingBatch(
		testContext, nil, testRunTally(),
	); done || !errors.Is(err, ErrInvalidProvenance) {
		t.Fatalf("invalid provenance bounded finish = %v, %v", done, err)
	}
	if done, err := checkpoint.FinishSeedingBatch(
		testContext, []byte("missing"), testRunTally(),
	); done || !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing bounded finish = %v, %v", done, err)
	}
	cancelled, cancel := context.WithCancel(testContext)
	cancel()
	if done, err := checkpoint.FinishSeedingBatch(
		cancelled, provenance, testRunTally(),
	); done || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled bounded finish = %v, %v", done, err)
	}
}

func TestCancelSeedManifestBatchDrainsManifestAndSettlesRun(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("bounded-cancel-manifest")
	pages := largeSeedManifestPages(seedManifestRowsPerTransaction + 1)
	identity := beginSeedManifest(t, checkpoint, provenance, pages)
	cancelled := true
	if err := checkpoint.UpdateControl(
		testContext,
		provenance,
		ControlUpdate{Cancelled: cancelled},
	); err != nil {
		t.Fatalf("mark manifest cancelled: %v", err)
	}
	done, err := checkpoint.CancelSeedManifestBatch(testContext, provenance)
	if err != nil || done {
		t.Fatalf("first manifest cancel = %v, %v", done, err)
	}
	done, err = checkpoint.CancelSeedManifestBatch(testContext, provenance)
	if err != nil || !done {
		t.Fatalf("final manifest cancel = %v, %v", done, err)
	}
	done, err = checkpoint.CancelSeedManifestBatch(testContext, provenance)
	if err != nil || !done {
		t.Fatalf("replayed manifest cancel = %v, %v", done, err)
	}
	state, err := checkpoint.Inspect(testContext, provenance, identity)
	if err != nil || state.Status != RunCompleted || state.Seeding || state.SeedManifest {
		t.Fatalf("cancelled manifest state = %+v, %v", state, err)
	}
}

func TestCancelSeedManifestBatchRejectsUnmarkedAndInvalidRuns(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("bounded-uncancelled-manifest")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	if done, err := checkpoint.CancelSeedManifestBatch(
		testContext, provenance,
	); done || !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("unmarked manifest cancel = %v, %v", done, err)
	}
	if done, err := checkpoint.CancelSeedManifestBatch(
		testContext, nil,
	); done || !errors.Is(err, ErrInvalidProvenance) {
		t.Fatalf("invalid provenance manifest cancel = %v, %v", done, err)
	}
	if done, err := checkpoint.CancelSeedManifestBatch(
		testContext, []byte("missing"),
	); done || !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("missing manifest cancel = %v, %v", done, err)
	}
	deleteSchemaBucket(t, checkpoint, seedManifestBucket)
	mutateRunRecord(t, checkpoint, provenance, func(record *runRecord) {
		record.Cancelled = true
		record.SeedManifest = true
	})
	if done, err := checkpoint.CancelSeedManifestBatch(
		testContext, provenance,
	); done || !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("missing bucket manifest cancel = %v, %v", done, err)
	}
}

func TestAdmissionBatchStateReadsVisitedAndHostStateExactly(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("admission-state")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	visited := testPage("https://state.example/visited", "state.example", "visited", 0)
	unvisited := testPage("https://state.example/unvisited", "state.example", "unvisited", 1)
	other := testPage("https://other.example/page", "other.example", "other", 2)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{visited},
	); err != nil ||
		admitted != 1 {
		t.Fatalf("admit state fixture = %d, %v", admitted, err)
	}
	state, err := checkpoint.AdmissionBatchState(
		testContext, provenance, []Page{visited, unvisited, other},
	)
	if err != nil || len(state.Visited) != 3 || !state.Visited[0] || state.Visited[1] ||
		state.Visited[2] {
		t.Fatalf("admission state = %+v, %v", state, err)
	}
	if len(state.HostStates) != 2 || state.HostStates[visited.Host].Pages != 1 ||
		state.HostStates[other.Host] != (HostState{}) {
		t.Fatalf("admission host states = %+v", state.HostStates)
	}
}

func TestAdmissionBatchStateRejectsInvalidAndCorruptRows(t *testing.T) {
	page := testPage("https://state.example/page", "state.example", "state", 0)
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("admission-state-errors")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	invalid := page
	invalid.Host = ""
	for name, pages := range map[string][]Page{
		"empty":     nil,
		"invalid":   {invalid},
		"oversized": make([]Page, RecoveryPageBatchSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := checkpoint.AdmissionBatchState(
				testContext,
				provenance,
				pages,
			); err == nil {
				t.Fatal("invalid admission state succeeded")
			}
		})
	}
	if _, err := checkpoint.AdmissionBatchState(
		testContext,
		nil,
		[]Page{page},
	); !errors.Is(
		err,
		ErrInvalidProvenance,
	) {
		t.Fatalf("invalid admission provenance error = %v", err)
	}
	if _, err := checkpoint.AdmissionBatchState(
		testContext,
		[]byte("missing"),
		[]Page{page},
	); !errors.Is(
		err,
		ErrRunNotFound,
	) {
		t.Fatalf("missing admission run error = %v", err)
	}
}

func TestCancelRecoveryPagesPersistsBoundedCancellation(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("cancel-recovery-pages")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	pages := boundedRecoveryTestPages(RecoveryPageBatchSize + 7)
	admitCheckpointTestPages(t, checkpoint, provenance, pages)
	if err := checkpoint.UpdateControl(
		testContext,
		provenance,
		ControlUpdate{Cancelled: true},
	); err != nil {
		t.Fatalf("mark recovery cancelled: %v", err)
	}
	removed, err := checkpoint.CancelRecoveryPages(testContext, provenance, 0, uint64(len(pages)))
	if err != nil || removed != uint64(len(pages)) {
		t.Fatalf("cancel recovery pages = %d, %v", removed, err)
	}
	state, err := checkpoint.Inspect(testContext, provenance, []byte("identity"))
	if err != nil || state.Pending != 0 {
		t.Fatalf("cancelled recovery state = %+v, %v", state, err)
	}
	removed, err = checkpoint.CancelRecoveryPages(
		testContext,
		provenance,
		uint64(len(pages)),
		uint64(len(pages)),
	)
	if err != nil || removed != 0 {
		t.Fatalf("empty recovery cancellation = %d, %v", removed, err)
	}
}

func TestCancelRecoveryPagesRejectsInvalidBoundariesAndState(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("cancel-recovery-errors")
	beginTestRun(t, checkpoint, provenance, []byte("identity"))
	page := testPage("https://cancel.example/page", "cancel.example", "cancel", 0)
	if _, err := checkpoint.Admit(testContext, provenance, []Page{page}); err != nil {
		t.Fatalf("admit cancellation page: %v", err)
	}
	for _, testCase := range []struct {
		name       string
		provenance []byte
		after      uint64
		upper      uint64
		want       error
	}{
		{name: "provenance", provenance: nil, want: ErrInvalidProvenance},
		{name: "order", provenance: provenance, after: 2, upper: 1, want: ErrInvalidPage},
		{name: "state", provenance: provenance, upper: 1, want: ErrCorruptCheckpoint},
		{name: "upper", provenance: provenance, upper: 2, want: ErrCorruptCheckpoint},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := checkpoint.CancelRecoveryPages(
				testContext, testCase.provenance, testCase.after, testCase.upper,
			); !errors.Is(err, testCase.want) {
				t.Fatalf("cancellation error = %v, want %v", err, testCase.want)
			}
		})
	}
}
