package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeGrower struct {
	calls  int
	splits int
	err    error
	signal chan struct{}
}

func (f *fakeGrower) GrowShards(context.Context, int) (int, error) {
	f.calls++
	if f.signal != nil {
		f.signal <- struct{}{}
	}

	return f.splits, f.err
}

type fakeAutosplit struct{ enabled bool }

func (f fakeAutosplit) AutosplitEnabled() bool { return f.enabled }

func TestGrowOnceSkipsWhenDisabled(t *testing.T) {
	grower := &fakeGrower{splits: 2}
	growOnce(context.Background(), grower, fakeAutosplit{enabled: false})
	if grower.calls != 0 {
		t.Fatalf("disabled autosplit still called grow %d times", grower.calls)
	}
}

func TestGrowOnceGrowsWhenEnabled(t *testing.T) {
	grower := &fakeGrower{splits: 1}
	growOnce(context.Background(), grower, fakeAutosplit{enabled: true})
	if grower.calls != 1 {
		t.Fatalf("enabled autosplit called grow %d times, want 1", grower.calls)
	}
}

func TestGrowOnceToleratesError(t *testing.T) {
	grower := &fakeGrower{err: errors.New("boom")}
	growOnce(context.Background(), grower, fakeAutosplit{enabled: true})
	if grower.calls != 1 {
		t.Fatalf("grow calls = %d, want 1", grower.calls)
	}
}

func TestRunShardGrowthLoopGrowsOnTick(t *testing.T) {
	ticks := make(chan time.Time)
	saved := newGrowthTicks
	newGrowthTicks = func() (<-chan time.Time, func()) { return ticks, func() {} }
	t.Cleanup(func() { newGrowthTicks = saved })

	grower := &fakeGrower{splits: 1, signal: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go runShardGrowthLoop(ctx, grower, fakeAutosplit{enabled: true})

	ticks <- time.Now()
	select {
	case <-grower.signal:
	case <-time.After(2 * time.Second):
		t.Fatal("tick did not trigger growth")
	}
}

func TestRunShardGrowthLoopStopsOnContext(t *testing.T) {
	saved := newGrowthTicks
	newGrowthTicks = func() (<-chan time.Time, func()) { return make(chan time.Time), func() {} }
	t.Cleanup(func() { newGrowthTicks = saved })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runShardGrowthLoop(ctx, &fakeGrower{}, fakeAutosplit{enabled: true})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not stop on context cancel")
	}
}
