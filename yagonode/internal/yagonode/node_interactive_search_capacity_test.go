package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"
)

// The gate was a flat 4 while the HTTP gate in front admitted far more, so a
// busy node queued the surplus and answered them empty.
func TestInteractiveSearchCapacityFollowsTheProcessorCount(t *testing.T) {
	for _, item := range []struct {
		name  string
		procs int
		want  int
	}{
		{name: "many cores raise the gate", procs: 16, want: 16},
		{name: "few cores hold the floor", procs: 2, want: interactiveSearchMinimumConcurrentWork},
		{name: "at the floor", procs: interactiveSearchMinimumConcurrentWork, want: interactiveSearchMinimumConcurrentWork},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := interactiveSearchCapacity(item.procs); got != item.want {
				t.Fatalf("interactiveSearchCapacity(%d) = %d, want %d", item.procs, got, item.want)
			}
		})
	}
}

// A caller that waits for the whole budget is guaranteed an empty answer: it is
// admitted with no time left to search. The wait has to leave the primary stage
// its budget.
func TestInteractiveSearchAdmissionWaitLeavesTimeToSearch(t *testing.T) {
	usable := interactiveSearchBudget - interactiveSearchCancellationGrace
	if interactiveSearchAdmissionWait >= usable {
		t.Fatalf(
			"admission wait %s consumes the whole %s work budget",
			interactiveSearchAdmissionWait,
			usable,
		)
	}
	// The parallel privacy mode runs the widest primary stage; a caller that
	// spent its wait queueing must still be able to run it.
	if remaining := usable - interactiveSearchAdmissionWait; remaining < webFallbackParallelExactStageBudget {
		t.Fatalf(
			"waiting for a slot leaves %s, less than the %s primary stage",
			remaining,
			webFallbackParallelExactStageBudget,
		)
	}
}

func TestAcquireWithinTakesAFreeSlot(t *testing.T) {
	admission := newInteractiveSearchAdmission(1)
	release, err := admission.acquireWithin(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("acquire within: %v", err)
	}
	release()
}

// A full gate reports capacity, not a deadline: the caller's own deadline has
// not passed, so calling it a timeout would send the operator hunting a slow
// index instead of a saturated node.
func TestAcquireWithinReportsCapacityWhenTheGateIsFull(t *testing.T) {
	admission := newInteractiveSearchAdmission(1)
	release, err := admission.acquireWithin(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	if _, err := admission.acquireWithin(context.Background(), time.Millisecond); !errors.Is(
		err,
		errInteractiveSearchCapacity,
	) {
		t.Fatalf("second acquire error = %v, want capacity exhaustion", err)
	}
}

func TestAcquireWithinRefusesAnAlreadyCancelledCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newInteractiveSearchAdmission(1).acquireWithin(ctx, time.Second); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("error = %v, want the caller's cancellation", err)
	}
}

func TestAcquireWithinStopsWhenTheCallerGoesAway(t *testing.T) {
	admission := newInteractiveSearchAdmission(1)
	release, err := admission.acquireWithin(context.Background(), time.Second)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	go cancel()
	if _, err := admission.acquireWithin(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the caller's cancellation", err)
	}
}

// A caller that does not state its wait must not be told the node is full while
// a slot sits open. Arming a timer with a non-positive duration fires it
// immediately, and the admission select would then resolve between a free slot
// and an expired deadline at random -- a coin flip that reported capacity
// exhaustion for roughly half of an idle node's callers.
func TestAcquireWithinTreatsAnUnstatedWaitAsTheProcessDefault(t *testing.T) {
	admission := newInteractiveSearchAdmission(1)
	for range 24 {
		release, err := admission.acquireWithin(context.Background(), 0)
		if err != nil {
			t.Fatalf("idle gate refused an unstated wait: %v", err)
		}
		release()
	}
}

// The unbounded acquire still serves the explain and recovery stages, and it
// must refuse a caller that has already gone away rather than hand it a slot
// that nobody will release. An idle gate makes this the only way to observe the
// guard: the slot would otherwise be free for the taking.
func TestAcquireRefusesACallerThatHasAlreadyGoneAway(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	release, err := newInteractiveSearchAdmission(1).acquire(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want the caller's cancellation", err)
	}
	if release != nil {
		t.Fatal("a cancelled caller was handed a slot to release")
	}
}
