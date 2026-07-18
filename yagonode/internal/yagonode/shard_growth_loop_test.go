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

	if f.err != nil || f.splits == 0 {
		return 0, f.err
	}
	f.splits--

	return 1, nil
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
	if grower.calls != 2 {
		t.Fatalf("enabled autosplit called grow %d times, want 2", grower.calls)
	}
}

func TestGrowOnceToleratesError(t *testing.T) {
	grower := &fakeGrower{err: errors.New("boom")}
	growOnce(context.Background(), grower, fakeAutosplit{enabled: true})
	if grower.calls != 1 {
		t.Fatalf("grow calls = %d, want 1", grower.calls)
	}
}

type stagedShardGrowthAdmission struct {
	allowed int
	calls   int
}

type headroomGrower struct {
	*fakeGrower
	headroom uint64
	err      error
}

func (grower *headroomGrower) ShardGrowthHeadroom(context.Context) (uint64, error) {
	return grower.headroom, grower.err
}

func (admission *stagedShardGrowthAdmission) CheckGrowth() error {
	admission.calls++
	if admission.calls > admission.allowed {
		return errors.New("pressure")
	}

	return nil
}

func TestGrowOnceChecksStorageBeforeEverySplit(t *testing.T) {
	grower := &fakeGrower{splits: 4}
	admission := &stagedShardGrowthAdmission{allowed: 1}
	growOnce(context.Background(), grower, fakeAutosplit{enabled: true}, admission)
	if grower.calls != 1 || admission.calls != 2 {
		t.Fatalf("growth/admission calls = %d/%d, want 1/2", grower.calls, admission.calls)
	}
}

func TestGrowOnceRequiresTemporaryCopyHeadroom(t *testing.T) {
	grower := &headroomGrower{fakeGrower: &fakeGrower{splits: 1}, headroom: 6 << 30}
	admission := &nodeGrowthAdmission{err: errors.New("insufficient headroom")}
	growOnce(t.Context(), grower, fakeAutosplit{enabled: true}, admission)
	if grower.calls != 0 || admission.requiredHeadroom != 6<<30 {
		t.Fatalf(
			"headroom growth calls=%d required=%d",
			grower.calls,
			admission.requiredHeadroom,
		)
	}
}

func TestGrowOnceSkipsWhenNoSplitNeedsHeadroom(t *testing.T) {
	grower := &headroomGrower{fakeGrower: &fakeGrower{splits: 1}}
	growOnce(t.Context(), grower, fakeAutosplit{enabled: true}, &nodeGrowthAdmission{})
	if grower.calls != 0 {
		t.Fatalf("no-op growth called store %d times", grower.calls)
	}
}

func TestGrowOnceStopsWhenHeadroomMeasurementFails(t *testing.T) {
	grower := &headroomGrower{
		fakeGrower: &fakeGrower{splits: 1},
		err:        errors.New("measurement failed"),
	}
	growOnce(t.Context(), grower, fakeAutosplit{enabled: true})
	if grower.calls != 0 {
		t.Fatalf("unmeasured growth called store %d times", grower.calls)
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
