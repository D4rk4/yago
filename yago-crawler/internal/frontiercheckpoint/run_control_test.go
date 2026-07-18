package frontiercheckpoint

import (
	"context"
	"errors"
	"testing"
)

func rateLimit(value uint32) *uint32 {
	return &value
}

type runControlFixture struct {
	checkpoint *FrontierCheckpoint
	provenance []byte
	identity   []byte
}

func requireRunControl(
	t *testing.T,
	state RunState,
	paused bool,
	cancelled bool,
	pagesPerMinute *uint32,
) {
	t.Helper()
	if state.Control.Paused != paused || state.Control.Cancelled != cancelled {
		t.Fatalf("run control = %+v", state.Control)
	}
	if pagesPerMinute == nil {
		if state.Control.PagesPerMinute != nil {
			t.Fatalf("pages per minute = %v, want unset", *state.Control.PagesPerMinute)
		}
		return
	}
	if state.Control.PagesPerMinute == nil ||
		*state.Control.PagesPerMinute != *pagesPerMinute {
		t.Fatalf("pages per minute = %v, want %d", state.Control.PagesPerMinute, *pagesPerMinute)
	}
}

func TestRunControlUpdatesPreserveIndependentFields(t *testing.T) {
	fixture := runControlFixture{
		checkpoint: openTestCheckpoint(t, testCheckpointPath(t)),
		provenance: []byte("controlled-run"),
		identity:   []byte("controlled-identity"),
	}
	beginTestRun(t, fixture.checkpoint, fixture.provenance, fixture.identity)
	state, err := fixture.checkpoint.Inspect(
		testContext,
		fixture.provenance,
		fixture.identity,
	)
	if err != nil || state.Status != RunActive || !state.Seeding || state.Failed {
		t.Fatalf("initial run state = %+v, %v", state, err)
	}
	requireRunControl(t, state, false, false, nil)

	paused := true
	state = fixture.updateAndInspect(t, ControlUpdate{Paused: &paused}, "pause run")
	requireRunControl(t, state, true, false, nil)

	explicitZero := uint32(0)
	state = fixture.updateAndInspect(
		t,
		ControlUpdate{PagesPerMinute: &explicitZero},
		"set explicit zero rate",
	)
	explicitZero = 99
	requireRunControl(t, state, true, false, rateLimit(0))

	state = fixture.updateAndInspect(t, ControlUpdate{Cancelled: true}, "cancel run")
	requireRunControl(t, state, true, true, rateLimit(0))

	resumed := false
	fixture.updateAndInspect(t, ControlUpdate{Paused: &resumed}, "resume run")
	nonzeroRate := uint32(37)
	state = fixture.updateAndInspect(
		t,
		ControlUpdate{PagesPerMinute: &nonzeroRate},
		"set nonzero rate",
	)
	requireRunControl(t, state, false, true, rateLimit(37))
	*state.Control.PagesPerMinute = 88
	reloaded, err := fixture.checkpoint.Inspect(
		testContext,
		fixture.provenance,
		fixture.identity,
	)
	if err != nil {
		t.Fatalf("reinspect controlled run: %v", err)
	}
	requireRunControl(t, reloaded, false, true, rateLimit(37))
	snapshot, err := fixture.checkpoint.Load(testContext, fixture.provenance)
	if err != nil || snapshot.Control.PagesPerMinute == nil ||
		*snapshot.Control.PagesPerMinute != 37 || snapshot.Control.Paused ||
		!snapshot.Control.Cancelled {
		t.Fatalf("snapshot control = %+v, %v", snapshot.Control, err)
	}
}

func (fixture runControlFixture) updateAndInspect(
	t *testing.T,
	update ControlUpdate,
	operation string,
) RunState {
	t.Helper()
	if err := fixture.checkpoint.UpdateControl(
		testContext,
		fixture.provenance,
		update,
	); err != nil {
		t.Fatalf("%s: %v", operation, err)
	}
	state, err := fixture.checkpoint.Inspect(
		testContext,
		fixture.provenance,
		fixture.identity,
	)
	if err != nil {
		t.Fatalf("inspect after %s: %v", operation, err)
	}

	return state
}

func TestInspectReportsFailedCompletedRun(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("failed-run")
	identity := []byte("failed-identity")
	beginTestRun(t, checkpoint, provenance, identity)
	page := testPage("https://example.com/page", "example.com", "observation", 0)
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		[]Page{page},
	); err != nil || admitted != 1 {
		t.Fatalf("admit page = %d, %v", admitted, err)
	}
	if err := checkpoint.FinishSeeding(testContext, provenance, testRunTally()); err != nil {
		t.Fatalf("finish seeding: %v", err)
	}
	if err := checkpoint.CompletePage(
		testContext,
		provenance,
		page.URL,
		testFailedPageCompletion(),
	); err != nil {
		t.Fatalf("complete failed page: %v", err)
	}
	state, err := checkpoint.Inspect(testContext, provenance, identity)
	if err != nil || state.Status != RunCompleted || state.Seeding || state.Failed ||
		state.Tally.Failed != 1 {
		t.Fatalf("failed completed state = %+v, %v", state, err)
	}
	paused := true
	if err := checkpoint.UpdateControl(
		testContext,
		provenance,
		ControlUpdate{Paused: &paused},
	); !errors.Is(err, ErrRunCompleted) {
		t.Fatalf("completed control update error = %v", err)
	}
}

func TestRunControlAndInspectionRejectInvalidState(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("validation-run")
	identity := []byte("validation-identity")
	if _, err := checkpoint.Inspect(
		testContext,
		nil,
		identity,
	); !errors.Is(err, ErrInvalidProvenance) {
		t.Fatalf("empty inspect provenance error = %v", err)
	}
	if _, err := checkpoint.Inspect(
		testContext,
		provenance,
		nil,
	); !errors.Is(err, ErrInvalidIdentity) {
		t.Fatalf("empty inspect identity error = %v", err)
	}
	state, err := checkpoint.Inspect(testContext, provenance, identity)
	if err != nil || state.Status != RunMissing {
		t.Fatalf("missing run state = %+v, %v", state, err)
	}
	beginTestRun(t, checkpoint, provenance, identity)
	if _, err := checkpoint.Inspect(
		testContext,
		provenance,
		[]byte("different-identity"),
	); !errors.Is(err, ErrProvenanceCollision) {
		t.Fatalf("inspect identity collision error = %v", err)
	}
	paused := true
	checks := []struct {
		name   string
		target error
		run    func() error
	}{
		{
			name:   "provenance",
			target: ErrInvalidProvenance,
			run: func() error {
				return checkpoint.UpdateControl(
					testContext, nil, ControlUpdate{Paused: &paused},
				)
			},
		},
		{
			name:   "empty update",
			target: ErrInvalidControl,
			run: func() error {
				return checkpoint.UpdateControl(testContext, provenance, ControlUpdate{})
			},
		},
		{
			name:   "missing run",
			target: ErrRunNotFound,
			run: func() error {
				return checkpoint.UpdateControl(
					testContext, []byte("missing"), ControlUpdate{Paused: &paused},
				)
			},
		},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, check.target) {
				t.Fatalf("control update error = %v, want %v", err, check.target)
			}
		})
	}
}

func TestRunControlAndInspectionPropagateContextAndClosedErrors(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("unavailable-run")
	identity := []byte("unavailable-identity")
	beginTestRun(t, checkpoint, provenance, identity)
	ctx, cancel := context.WithCancel(testContext)
	cancel()
	paused := true
	if _, err := checkpoint.Inspect(ctx, provenance, identity); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled inspect error = %v", err)
	}
	if err := checkpoint.UpdateControl(
		ctx,
		provenance,
		ControlUpdate{Paused: &paused},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled control update error = %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close checkpoint: %v", err)
	}
	if _, err := checkpoint.Inspect(
		testContext,
		provenance,
		identity,
	); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed inspect error = %v", err)
	}
	if err := checkpoint.UpdateControl(
		testContext,
		provenance,
		ControlUpdate{Paused: &paused},
	); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed control update error = %v", err)
	}
}
