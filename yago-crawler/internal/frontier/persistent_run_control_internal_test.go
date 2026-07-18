package frontier

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func requireControlUpdate(
	t *testing.T,
	update frontiercheckpoint.ControlUpdate,
	paused *bool,
	cancelled bool,
	pagesPerMinute *uint32,
) {
	t.Helper()
	if update.Cancelled != cancelled {
		t.Fatalf("cancelled update = %t, want %t", update.Cancelled, cancelled)
	}
	if paused == nil {
		if update.Paused != nil {
			t.Fatalf("paused update = %t, want unset", *update.Paused)
		}
	} else if update.Paused == nil || *update.Paused != *paused {
		t.Fatalf("paused update = %v, want %t", update.Paused, *paused)
	}
	if pagesPerMinute == nil {
		if update.PagesPerMinute != nil {
			t.Fatalf("rate update = %d, want unset", *update.PagesPerMinute)
		}
	} else if update.PagesPerMinute == nil ||
		*update.PagesPerMinute != *pagesPerMinute {
		t.Fatalf("rate update = %v, want %d", update.PagesPerMinute, *pagesPerMinute)
	}
}

func boolValue(value bool) *bool {
	return &value
}

func rateValue(value uint32) *uint32 {
	return &value
}

func TestActivePersistentControlsCommitBeforeMemoryMutation(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("active-control-identity")
	provenance := []byte("active-control-provenance")
	checkpoint := &scriptedCheckpoint{
		snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
	}
	settled := make(chan bool, 1)
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://example.com/controlled"),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		func(succeeded bool) { settled <- succeeded },
	)
	if !frontier.PauseControl(provenance) {
		t.Fatal("active pause was not durably applied")
	}
	if _, paused := frontier.paused[string(provenance)]; !paused {
		t.Fatal("committed pause was not applied")
	}
	if !frontier.SetRateControl(provenance, 37) {
		t.Fatal("active rate was not durably applied")
	}
	if got := frontier.EffectivePagesPerMinute(provenance); got != 37 {
		t.Fatalf("committed rate = %d, want 37", got)
	}
	if !frontier.SetRateControl(provenance, 0) {
		t.Fatal("active zero rate was not durably applied")
	}
	if got := frontier.EffectivePagesPerMinute(provenance); got != 0 {
		t.Fatalf("committed zero rate = %d", got)
	}
	if !frontier.ResumeControl(provenance) {
		t.Fatal("active resume was not durably applied")
	}
	if _, paused := frontier.paused[string(provenance)]; paused {
		t.Fatal("committed resume left run paused")
	}
	if !frontier.CancelControl(provenance) {
		t.Fatal("active cancellation was not durably applied")
	}
	if !frontier.WasCancelled(provenance) {
		t.Fatal("committed cancellation was not applied")
	}
	if !checkpointSettlement(t, settled) {
		t.Fatal("cancelled run did not drain cleanly")
	}
	if len(checkpoint.controlUpdates) != 5 {
		t.Fatalf("control updates = %d, want 5", len(checkpoint.controlUpdates))
	}
	requireControlUpdate(t, checkpoint.controlUpdates[0], boolValue(true), false, nil)
	requireControlUpdate(t, checkpoint.controlUpdates[1], nil, false, rateValue(37))
	requireControlUpdate(t, checkpoint.controlUpdates[2], nil, false, rateValue(0))
	requireControlUpdate(t, checkpoint.controlUpdates[3], boolValue(false), false, nil)
	requireControlUpdate(t, checkpoint.controlUpdates[4], boolValue(true), true, nil)
}

func TestActivePersistentControlFailureIsFailClosed(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("failed-control-identity")
	provenance := []byte("failed-control-provenance")
	controlFailure := errors.New("persist active control")
	checkpoint := &scriptedCheckpoint{
		snapshot:     checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
		controlError: controlFailure,
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests:      internalRequests(profile, "https://example.com/fail-closed"),
			Provenance:    provenance,
			OrderIdentity: identity,
		},
		profile,
		nil,
	)
	if frontier.PauseControl(provenance) {
		t.Fatal("failed pause reported durable application")
	}
	if !errors.Is(frontier.CheckpointFailure(), controlFailure) {
		t.Fatalf("control checkpoint failure = %v", frontier.CheckpointFailure())
	}
	if _, paused := frontier.paused[string(provenance)]; paused {
		t.Fatal("failed pause mutated memory")
	}
	if len(checkpoint.controlUpdates) != 1 {
		t.Fatalf("failed control updates = %d, want 1", len(checkpoint.controlUpdates))
	}
	if frontier.RunPending(seeded.RunID) != 1 {
		t.Fatalf("failed control pending = %d, want 1", frontier.RunPending(seeded.RunID))
	}
	if _, ok := frontier.Take(t.Context()); ok {
		t.Fatal("frontier dispatched after control persistence failure")
	}
}

func TestPreRegistrationControlsPersistAsOneMergedUpdate(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("pending-control-identity")
	provenance := []byte("pending-control-provenance")
	checkpoint := &scriptedCheckpoint{
		snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	if frontier.PauseControl(provenance) || frontier.ResumeControl(provenance) ||
		frontier.SetRateControl(provenance, 37) || frontier.SetRateControl(provenance, 0) ||
		frontier.CancelControl(provenance) {
		t.Fatal("pre-registration control reported durable application")
	}
	settled := make(chan bool, 1)
	frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(succeeded bool) { settled <- succeeded },
	)
	if !checkpointSettlement(t, settled) {
		t.Fatal("pre-cancelled run did not drain cleanly")
	}
	if len(checkpoint.controlUpdates) != 1 {
		t.Fatalf("merged control updates = %d, want 1", len(checkpoint.controlUpdates))
	}
	requireControlUpdate(
		t,
		checkpoint.controlUpdates[0],
		boolValue(true),
		true,
		rateValue(0),
	)
	if _, found := frontier.pendingControl[string(provenance)]; found {
		t.Fatal("persisted pending control was retained")
	}
	if _, found := frontier.controlSeen[string(provenance)]; found {
		t.Fatal("persisted pending control timestamp was retained")
	}
}

type orderedControlCheckpoint struct {
	*scriptedCheckpoint
	calls         atomic.Int64
	firstStarted  chan struct{}
	secondStarted chan struct{}
	releaseFirst  chan struct{}
}

func (checkpoint *orderedControlCheckpoint) UpdateControl(
	ctx context.Context,
	provenance []byte,
	update frontiercheckpoint.ControlUpdate,
) error {
	switch checkpoint.calls.Add(1) {
	case 1:
		close(checkpoint.firstStarted)
		select {
		case <-checkpoint.releaseFirst:
		case <-ctx.Done():
			return fmt.Errorf("wait for ordered control release: %w", ctx.Err())
		}
	case 2:
		close(checkpoint.secondStarted)
	}

	return checkpoint.scriptedCheckpoint.UpdateControl(ctx, provenance, update)
}

func TestPendingControlCannotOverwriteNewerActiveControl(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("ordered-control-identity")
	provenance := []byte("ordered-control-provenance")
	checkpoint := &orderedControlCheckpoint{
		scriptedCheckpoint: &scriptedCheckpoint{
			snapshot: checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
		},
		firstStarted:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
	}
	frontier := NewFrontier(2, nil, WithCheckpoint(checkpoint))
	frontier.Pause(provenance)
	seeded := make(chan SeededRun, 1)
	go func() {
		seeded <- frontier.SeedRunWithPriority(
			context.Background(),
			CrawlRunSeed{
				Requests:      internalRequests(profile, "https://example.com/ordered"),
				Provenance:    provenance,
				OrderIdentity: identity,
			},
			profile,
			func(bool) {},
		)
	}()
	select {
	case <-checkpoint.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("pending pause did not reach checkpoint")
	}
	resumed := make(chan struct{})
	go func() {
		frontier.Resume(provenance)
		close(resumed)
	}()
	select {
	case <-checkpoint.secondStarted:
		t.Fatal("newer resume bypassed pending control ordering")
	case <-time.After(20 * time.Millisecond):
	}
	close(checkpoint.releaseFirst)
	var run SeededRun
	select {
	case run = <-seeded:
	case <-time.After(time.Second):
		t.Fatal("seed did not finish after pending control commit")
	}
	select {
	case <-resumed:
	case <-time.After(time.Second):
		t.Fatal("resume did not finish after pending control commit")
	}
	if len(checkpoint.controlUpdates) != 2 {
		t.Fatalf("control updates = %d, want pause then resume", len(checkpoint.controlUpdates))
	}
	requireControlUpdate(t, checkpoint.controlUpdates[0], boolValue(true), false, nil)
	requireControlUpdate(t, checkpoint.controlUpdates[1], boolValue(false), false, nil)
	if _, paused := frontier.paused[string(provenance)]; paused {
		t.Fatal("older pending pause overwrote newer resume")
	}
	job, ok := frontier.Take(t.Context())
	if !ok || job.RunID != run.RunID {
		t.Fatalf("resumed job = %+v, %t", job, ok)
	}
	frontier.Done(job, successfulPageOutcome())
}

func TestCancelledRunCannotDispatchBeforeQueueDrain(t *testing.T) {
	profile := internalProfile(t)
	provenance := []byte("cancel-dispatch-boundary")
	frontier := NewFrontier(2, nil)
	seeded := frontier.SeedRun(
		t.Context(),
		internalRequests(profile, "https://example.com/one", "https://example.com/two"),
		provenance,
		profile,
		func(bool) {},
	)
	frontier.mu.Lock()
	frontier.state.runs[seeded.RunID].cancelled = true
	frontier.state.cancelled[string(provenance)] = struct{}{}
	frontier.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if job, ok := frontier.Take(ctx); ok {
		t.Fatalf("cancelled run dispatched %q before queue drain", job.URL)
	}
}

func TestPreRegistrationControlFailureNacksEmptyRun(t *testing.T) {
	profile := internalProfile(t)
	identity := []byte("pending-failure-identity")
	provenance := []byte("pending-failure-provenance")
	controlFailure := errors.New("persist pending control")
	checkpoint := &scriptedCheckpoint{
		snapshot:     checkpointSnapshot(identity, yagocrawlcontract.CrawlOrderPriorityNormal),
		controlError: controlFailure,
	}
	frontier := NewFrontier(1, nil, WithCheckpoint(checkpoint))
	frontier.Pause(provenance)
	settled := make(chan bool, 1)
	frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{Provenance: provenance, OrderIdentity: identity},
		profile,
		func(succeeded bool) { settled <- succeeded },
	)
	if checkpointSettlement(t, settled) {
		t.Fatal("run with failed pending control persistence succeeded")
	}
	if !errors.Is(frontier.CheckpointFailure(), controlFailure) {
		t.Fatalf("pending control failure = %v", frontier.CheckpointFailure())
	}
	if len(checkpoint.controlUpdates) != 1 {
		t.Fatalf("pending failure updates = %d, want 1", len(checkpoint.controlUpdates))
	}
}

func TestCheckpointControlRecoveryRestoresPauseCancelAndRates(t *testing.T) {
	profile := internalProfile(t)
	frontier := NewFrontier(1, nil)

	pausedRunID := uuid.New()
	pausedProvenance := []byte("paused-recovery")
	frontier.state.beginRun(pausedRunID, pausedProvenance, profile, nil)
	frontier.restoreControlStateLocked(pausedRunID, frontiercheckpoint.RunControl{
		Paused:         true,
		PagesPerMinute: rateValue(0),
	})
	if _, paused := frontier.paused[string(pausedProvenance)]; !paused {
		t.Fatal("checkpoint pause was not restored")
	}
	if got := frontier.EffectivePagesPerMinute(pausedProvenance); got != 0 {
		t.Fatalf("restored explicit zero rate = %d", got)
	}

	cancelledRunID := uuid.New()
	cancelledProvenance := []byte("cancelled-recovery")
	frontier.state.beginRun(cancelledRunID, cancelledProvenance, profile, nil)
	frontier.restoreControlStateLocked(cancelledRunID, frontiercheckpoint.RunControl{
		Cancelled:      true,
		PagesPerMinute: rateValue(29),
	})
	if !frontier.WasCancelled(cancelledProvenance) ||
		!frontier.state.runs[cancelledRunID].cancelled {
		t.Fatal("checkpoint cancellation was not restored")
	}
	if got := frontier.EffectivePagesPerMinute(cancelledProvenance); got != 29 {
		t.Fatalf("restored nonzero rate = %d, want 29", got)
	}
	if due := frontier.rateNextDue[string(cancelledProvenance)]; !due.After(time.Now()) {
		t.Fatalf("restored rate deadline = %v, want a future deadline", due)
	}
	if frontier.cancelRuns[string(cancelledProvenance)] != 1 {
		t.Fatalf(
			"restored cancel run references = %d, want 1",
			frontier.cancelRuns[string(cancelledProvenance)],
		)
	}
}

func TestPendingControlWithoutCheckpointAppliesAndActiveRetentionDropsTimestamp(t *testing.T) {
	profile := internalProfile(t)
	frontier := NewFrontier(1, nil)
	provenance := []byte("memory-control")
	runID := uuid.New()
	frontier.state.beginRun(runID, provenance, profile, nil)
	frontier.pendingControl[string(provenance)] = rateControlUpdate(13)
	frontier.controlSeen[string(provenance)] = time.Now()
	frontier.persistPendingControl(runID)
	if got := frontier.EffectivePagesPerMinute(provenance); got != 13 {
		t.Fatalf("memory-only pending rate = %d, want 13", got)
	}
	frontier.controlSeen[string(provenance)] = time.Now()
	frontier.retainPendingControlLocked(string(provenance))
	if _, found := frontier.controlSeen[string(provenance)]; found {
		t.Fatal("active run retained a pending-control timestamp")
	}
}

func TestPersistPendingControlIgnoresMissingRun(t *testing.T) {
	frontier := NewFrontier(1, nil)
	frontier.persistPendingControl(uuid.New())
	if len(frontier.pendingControl) != 0 || len(frontier.controlSeen) != 0 {
		t.Fatalf(
			"missing run changed controls: pending=%v seen=%v",
			frontier.pendingControl,
			frontier.controlSeen,
		)
	}
}
