package peerroster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// readPriorityEngine records whether each read announced itself as background work,
// which is what decides whether concurrent writers yield to it.
type readPriorityEngine struct {
	*scriptedEngine
	backgroundReads int
	foregroundReads int
}

func (e *readPriorityEngine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if vault.IsBackgroundRead(ctx) {
		e.backgroundReads++
	} else {
		e.foregroundReads++
	}

	return e.scriptedEngine.View(ctx, fn)
}

// openRosterBeyondCapacity yields a roster holding more peers than its reservoir
// admits, with the reads taken while opening it already accounted for.
func openRosterBeyondCapacity(t *testing.T) (*roster, *readPriorityEngine) {
	t.Helper()
	engine := &readPriorityEngine{scriptedEngine: newScriptedEngine()}
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	opened, err := Open(
		t.Context(),
		storage,
		internalHashFor("local"),
		func() time.Time { return time.Unix(100, 0) },
		Capacity{Reservoir: 8, Active: 4},
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	r := opened.(*roster)
	r.Discover(
		t.Context(),
		internalSeed(t, "first", "203.0.113.1"),
		internalSeed(t, "second", "203.0.113.2"),
	)
	r.reservoirCap = 1
	engine.backgroundReads, engine.foregroundReads = 0, 0

	return r, engine
}

func TestTrimOverflowReadsAsBackgroundWork(t *testing.T) {
	r, engine := openRosterBeyondCapacity(t)

	changed, err := r.trimOverflow(t.Context())

	if err != nil || !changed {
		t.Fatalf("overflow trim = changed %t error %v", changed, err)
	}
	if engine.foregroundReads != 0 || engine.backgroundReads != 2 {
		t.Fatalf(
			"overflow trim reads = %d foreground %d background, want 0 foreground 2 background",
			engine.foregroundReads,
			engine.backgroundReads,
		)
	}
}

func TestKnownPeerCountReadsAsForegroundWork(t *testing.T) {
	r, engine := openRosterBeyondCapacity(t)

	if known := r.KnownPeerCount(t.Context()); known != 2 {
		t.Fatalf("known peers = %d, want 2", known)
	}
	if engine.foregroundReads != 1 || engine.backgroundReads != 0 {
		t.Fatalf(
			"known peer count reads = %d foreground %d background, want 1 foreground 0 background",
			engine.foregroundReads,
			engine.backgroundReads,
		)
	}
}

func TestTrimOverflowBackgroundReadsStillHonorCancellation(t *testing.T) {
	r, engine := openRosterBeyondCapacity(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := r.trimOverflow(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled overflow trim error = %v, want context canceled", err)
	}
	// The trim also writes, and a write refused by the same cancellation would report
	// the identical error, so the read has to prove separately that it never ran.
	if engine.backgroundReads != 0 {
		t.Fatalf(
			"canceled overflow trim still opened %d background reads",
			engine.backgroundReads,
		)
	}
}
