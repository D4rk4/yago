package yagonode

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type stubCompactor struct {
	mu     sync.Mutex
	calls  int
	result vault.CompactResult
	err    error
}

func (s *stubCompactor) Compact(context.Context) (vault.CompactResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++

	return s.result, s.err
}

func (s *stubCompactor) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.calls
}

func swapCompactionTicks(ch <-chan time.Time) func() {
	prev := newCompactionTicks
	newCompactionTicks = func() (<-chan time.Time, func()) { return ch, func() {} }

	return func() { newCompactionTicks = prev }
}

func TestAdvanceCompaction(t *testing.T) {
	base := time.Unix(1_000_000, 0)
	cases := []struct {
		name        string
		interval    time.Duration
		last, now   time.Time
		wantCompact bool
		wantLast    time.Time
	}{
		{"disabled clears the baseline", 0, base, base.Add(time.Hour), false, time.Time{}},
		{"first observation sets the baseline", time.Hour, time.Time{}, base, false, base},
		{
			"not yet due keeps the baseline",
			time.Hour,
			base,
			base.Add(30 * time.Minute),
			false,
			base,
		},
		{"exactly due compacts", time.Hour, base, base.Add(time.Hour), true, base.Add(time.Hour)},
		{
			"overdue compacts and resets",
			time.Hour,
			base,
			base.Add(3 * time.Hour),
			true,
			base.Add(3 * time.Hour),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubCompactor{}
			got := advanceCompaction(context.Background(), store, tc.interval, tc.last, tc.now)
			if !got.Equal(tc.wantLast) {
				t.Fatalf("baseline = %v, want %v", got, tc.wantLast)
			}
			if compacted := store.count() == 1; compacted != tc.wantCompact {
				t.Fatalf("compacted = %v, want %v", compacted, tc.wantCompact)
			}
		})
	}
}

func TestRunCompactionLoopCompactsOncePerInterval(t *testing.T) {
	ticks := make(chan time.Time)
	defer swapCompactionTicks(ticks)()

	store := &stubCompactor{result: vault.CompactResult{ShardsCompacted: 1, BytesReclaimed: 4096}}
	toggles := &runtimeToggles{}
	toggles.SetCompactionInterval(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runCompactionLoop(ctx, store, toggles)
		close(done)
	}()

	// Each send unblocks only once the loop has finished processing the previous
	// tick, giving a happens-before edge to assert against.
	base := time.Unix(1_000_000, 0)
	ticks <- base                       // establishes the baseline, no compaction
	ticks <- base.Add(30 * time.Minute) // not due yet
	ticks <- base.Add(time.Hour)        // due → one compaction
	ticks <- base.Add(90 * time.Minute) // < interval since reset → still one

	if got := store.count(); got != 1 {
		t.Fatalf("compaction count = %d, want 1", got)
	}

	cancel()
	<-done
}

func TestRunCompactionLoopSkipsWhenDisabled(t *testing.T) {
	ticks := make(chan time.Time)
	defer swapCompactionTicks(ticks)()

	store := &stubCompactor{result: vault.CompactResult{ShardsCompacted: 1}}
	toggles := &runtimeToggles{} // interval defaults to 0 = off

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runCompactionLoop(ctx, store, toggles)
		close(done)
	}()

	base := time.Unix(1_000_000, 0)
	ticks <- base
	ticks <- base.Add(48 * time.Hour)
	ticks <- base.Add(96 * time.Hour) // sync point: prior ticks fully processed

	if got := store.count(); got != 0 {
		t.Fatalf("disabled compaction ran %d times, want 0", got)
	}

	cancel()
	<-done
}
