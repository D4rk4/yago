package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSyncer struct {
	enabled bool
	calls   int
	err     error
	signal  chan struct{}
}

func (f *fakeSyncer) DeferredFsyncEnabled() bool { return f.enabled }

func (f *fakeSyncer) SyncShards(context.Context) error {
	f.calls++
	if f.signal != nil {
		f.signal <- struct{}{}
	}

	return f.err
}

func TestSyncOnceSkipsWhenDisabled(t *testing.T) {
	syncer := &fakeSyncer{enabled: false}
	syncOnce(context.Background(), syncer)
	if syncer.calls != 0 {
		t.Fatalf("disabled deferred fsync still flushed %d times", syncer.calls)
	}
}

func TestSyncOnceFlushesWhenEnabled(t *testing.T) {
	syncer := &fakeSyncer{enabled: true}
	syncOnce(context.Background(), syncer)
	if syncer.calls != 1 {
		t.Fatalf("enabled deferred fsync flushed %d times, want 1", syncer.calls)
	}
}

func TestSyncOnceToleratesError(t *testing.T) {
	syncer := &fakeSyncer{enabled: true, err: errors.New("boom")}
	syncOnce(context.Background(), syncer)
	if syncer.calls != 1 {
		t.Fatalf("flush calls = %d, want 1", syncer.calls)
	}
}

func TestRunDeferredSyncLoopFlushesOnTick(t *testing.T) {
	ticks := make(chan time.Time)
	saved := newDeferredSyncTicks
	newDeferredSyncTicks = func() (<-chan time.Time, func()) { return ticks, func() {} }
	t.Cleanup(func() { newDeferredSyncTicks = saved })

	syncer := &fakeSyncer{enabled: true, signal: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go runDeferredSyncLoop(ctx, syncer)

	ticks <- time.Now()
	select {
	case <-syncer.signal:
	case <-time.After(2 * time.Second):
		t.Fatal("tick did not trigger a flush")
	}
}

func TestRunDeferredSyncLoopStopsOnContext(t *testing.T) {
	saved := newDeferredSyncTicks
	newDeferredSyncTicks = func() (<-chan time.Time, func()) { return make(chan time.Time), func() {} }
	t.Cleanup(func() { newDeferredSyncTicks = saved })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runDeferredSyncLoop(ctx, &fakeSyncer{enabled: true})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not stop on context cancel")
	}
}
