package shardvault

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type cancellationRaceContext struct {
	context.Context
	cancelAt int32
	calls    atomic.Int32
}

func (c *cancellationRaceContext) Err() error {
	if c.calls.Add(1) >= c.cancelAt {
		return context.Canceled
	}

	return nil
}

func TestViewDoesNotEnterAfterContextCancellation(t *testing.T) {
	engine := &engine{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	err := engine.View(ctx, func(vault.EngineTxn) error {
		called = true

		return nil
	})
	if !errors.Is(err, context.Canceled) || called || engine.viewsInFlight.Load() != 0 {
		t.Fatalf("error = %v, called = %t, views = %d", err, called, engine.viewsInFlight.Load())
	}
}

func TestViewCancelsWhileWaitingForGlobalWriter(t *testing.T) {
	engine := &engine{}
	engine.globalGate.Lock()
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	var called atomic.Bool
	go func() {
		result <- engine.View(ctx, func(vault.EngineTxn) error {
			called.Store(true)

			return nil
		})
	}()
	waitForViews(t, engine, 1)
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) || called.Load() {
		t.Fatalf("error = %v, called = %t", err, called.Load())
	}
	engine.globalGate.Unlock()
}

func TestViewEntersAfterGlobalWriterReleases(t *testing.T) {
	engine := &engine{}
	engine.globalGate.Lock()
	result := make(chan error, 1)
	var called atomic.Bool
	go func() {
		result <- engine.View(t.Context(), func(vault.EngineTxn) error {
			called.Store(true)

			return nil
		})
	}()
	waitForViews(t, engine, 1)
	time.Sleep(2 * globalReadRetryInterval)
	engine.globalGate.Unlock()
	if err := <-result; err != nil || !called.Load() {
		t.Fatalf("error = %v, called = %t", err, called.Load())
	}
}

func TestBackgroundViewDoesNotClaimInteractiveReadPriority(t *testing.T) {
	engine := &engine{}
	err := engine.View(vault.BackgroundRead(t.Context()), func(vault.EngineTxn) error {
		if got := engine.viewsInFlight.Load(); got != 0 {
			t.Fatalf("background views = %d", got)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = engine.View(t.Context(), func(vault.EngineTxn) error {
		if got := engine.viewsInFlight.Load(); got != 1 {
			t.Fatalf("interactive views = %d", got)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGlobalReadReleasesGateWhenCancellationRacesAcquisition(t *testing.T) {
	ctx := &cancellationRaceContext{Context: context.Background(), cancelAt: 2}
	gate := &sync.RWMutex{}
	acquired, err := tryAcquireGlobalRead(ctx, gate)
	if acquired || !errors.Is(err, context.Canceled) {
		t.Fatalf("acquired = %t, error = %v", acquired, err)
	}
	if !gate.TryLock() {
		t.Fatal("canceled read retained the gate")
	}
	gate.Unlock()
}

func TestGlobalReadStopsWhenCancellationRacesRetry(t *testing.T) {
	ctx := &cancellationRaceContext{Context: context.Background(), cancelAt: 2}
	gate := &sync.RWMutex{}
	gate.Lock()
	err := acquireGlobalRead(ctx, gate)
	gate.Unlock()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestUsedBytesStopsBetweenShardMeasurements(t *testing.T) {
	engine := &engine{shards: make([]*bolt.DB, 1)}
	ctx := &cancellationRaceContext{Context: context.Background(), cancelAt: 3}
	if _, err := engine.UsedBytes(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func waitForViews(t *testing.T, engine *engine, want int64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for engine.viewsInFlight.Load() != want {
		if time.Now().After(deadline) {
			t.Fatalf("views = %d", engine.viewsInFlight.Load())
		}
		time.Sleep(time.Millisecond)
	}
}
