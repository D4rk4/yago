package readpriority

import (
	"context"
	"testing"
	"time"
)

func TestAwaitReturnsZeroWhenDisabledOrIdle(t *testing.T) {
	if d := Await(context.Background(), 0, func() bool { return true }); d != 0 {
		t.Fatalf("non-positive budget must not defer: %v", d)
	}
	if d := Await(context.Background(), -time.Second, func() bool { return true }); d != 0 {
		t.Fatalf("negative budget must not defer: %v", d)
	}
	if d := Await(context.Background(), DefaultBudget, nil); d != 0 {
		t.Fatalf("nil readsBusy must not defer: %v", d)
	}
	if d := Await(context.Background(), DefaultBudget, func() bool { return false }); d != 0 {
		t.Fatalf("no reads in flight must not defer: %v", d)
	}
}

func TestAwaitDefersWhileReadsBusy(t *testing.T) {
	restore := sleepFor
	t.Cleanup(func() { sleepFor = restore })
	slept := time.Duration(0)
	sleepFor = func(d time.Duration) { slept += d }

	calls := 0
	busy := func() bool { calls++; return calls <= 3 }
	waited := Await(context.Background(), DefaultBudget, busy)
	if waited != 3*step {
		t.Fatalf("waited %v, want three poll steps", waited)
	}
	if slept != 3*step {
		t.Fatalf("slept %v, want three poll steps", slept)
	}
}

func TestAwaitStopsAtBudget(t *testing.T) {
	restore := sleepFor
	t.Cleanup(func() { sleepFor = restore })
	sleepFor = func(time.Duration) {}

	waited := Await(context.Background(), 3*step, func() bool { return true })
	if waited != 3*step {
		t.Fatalf("waited %v, want the budget ceiling", waited)
	}
}

func TestAwaitStopsOnCancelledContext(t *testing.T) {
	restore := sleepFor
	t.Cleanup(func() { sleepFor = restore })
	sleepFor = func(time.Duration) {}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waited := Await(ctx, DefaultBudget, func() bool { return true }); waited != 0 {
		t.Fatalf("cancelled context must stop before the first sleep: %v", waited)
	}
}
