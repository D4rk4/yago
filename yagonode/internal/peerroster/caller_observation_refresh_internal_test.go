package peerroster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

type callerObservationClock struct {
	now time.Time
}

func openCallerObservationRoster(
	t *testing.T,
) (*roster, *scriptedEngine, *callerObservationClock) {
	t.Helper()
	r, engine := openScriptedRoster(t, 8, 4)
	clock := &callerObservationClock{now: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
	r.now = func() time.Time { return clock.now }

	return r, engine, clock
}

func observeCallerCountingUpdates(
	t *testing.T,
	r *roster,
	engine *scriptedEngine,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
) int32 {
	t.Helper()
	before := engine.updates.Load()
	r.ObserveCaller(t.Context(), caller, classification)

	return engine.updates.Load() - before
}

func TestRepeatedCallerObservationWithinRefreshIntervalSkipsTheWrite(t *testing.T) {
	r, engine, clock := openCallerObservationRoster(t)
	caller := internalSeed(t, "caller", "203.0.113.1")
	caller.RWICount = yagomodel.Some(10)
	if updates := observeCallerCountingUpdates(
		t, r, engine, caller, yagomodel.PeerSenior,
	); updates != 1 {
		t.Fatalf("first observation updates = %d, want 1", updates)
	}

	clock.now = clock.now.Add(callerObservationRefreshInterval - time.Second)
	repeated := caller.Copy()
	repeated.RWICount = yagomodel.Some(20)
	repeated.Uptime = yagomodel.Some(7)

	if updates := observeCallerCountingUpdates(
		t, r, engine, repeated, yagomodel.PeerSenior,
	); updates != 0 {
		t.Fatalf("repeated observation updates = %d, want 0", updates)
	}
	r.mu.Lock()
	active := r.active[caller.Hash]
	r.mu.Unlock()
	rwiCount, _ := active.seed.RWICount.Get()
	if !active.lastSeen.Equal(clock.now) || rwiCount != 20 {
		t.Fatalf("active entry = %v / %d, want refreshed in memory", active.lastSeen, rwiCount)
	}
}

func TestCallerObservationWritesWhenStoredFactsChange(t *testing.T) {
	changes := map[string]func(*testing.T, *roster, *callerObservationClock, *yagomodel.Seed){
		"refresh interval elapsed": func(
			_ *testing.T, _ *roster, clock *callerObservationClock, _ *yagomodel.Seed,
		) {
			clock.now = clock.now.Add(callerObservationRefreshInterval)
		},
		"clock moved backwards": func(
			_ *testing.T, _ *roster, clock *callerObservationClock, _ *yagomodel.Seed,
		) {
			clock.now = clock.now.Add(-time.Second)
		},
		"contact port changed": func(
			_ *testing.T, _ *roster, _ *callerObservationClock, caller *yagomodel.Seed,
		) {
			caller.Port = yagomodel.Some(yagomodel.Port(8091))
		},
		"peer cooling after transport failure": func(
			t *testing.T, r *roster, _ *callerObservationClock, caller *yagomodel.Seed,
		) {
			r.ConfirmUnreachable(t.Context(), caller.Hash)
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			r, engine, clock := openCallerObservationRoster(t)
			caller := internalSeed(t, "caller", "203.0.113.1")
			r.ObserveCaller(t.Context(), caller, yagomodel.PeerSenior)

			change(t, r, clock, &caller)

			if updates := observeCallerCountingUpdates(
				t, r, engine, caller, yagomodel.PeerSenior,
			); updates != 1 {
				t.Fatalf("observation updates = %d, want 1", updates)
			}
		})
	}
}

func TestVerifiedCallerObservationUpgradesAnUnverifiedPeer(t *testing.T) {
	r, engine, _ := openCallerObservationRoster(t)
	caller := internalSeed(t, "caller", "203.0.113.1")
	caller.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.Discover(t.Context(), caller)

	if updates := observeCallerCountingUpdates(
		t, r, engine, caller, yagomodel.PeerSenior,
	); updates != 1 {
		t.Fatalf("verified observation of a discovered peer updates = %d, want 1", updates)
	}
}

func TestJuniorCallerObservationNeverWritesAKnownPeer(t *testing.T) {
	r, engine, clock := openCallerObservationRoster(t)
	caller := internalSeed(t, "caller", "203.0.113.1")
	r.ObserveCaller(t.Context(), caller, yagomodel.PeerSenior)
	clock.now = clock.now.Add(callerObservationRefreshInterval)

	if updates := observeCallerCountingUpdates(
		t, r, engine, caller, yagomodel.PeerJunior,
	); updates != 0 {
		t.Fatalf("junior observation of a known peer updates = %d, want 0", updates)
	}
	if updates := observeCallerCountingUpdates(
		t, r, engine, internalSeed(t, "newcomer", "203.0.113.2"), yagomodel.PeerJunior,
	); updates != 1 {
		t.Fatalf("junior observation of a new peer updates = %d, want 1", updates)
	}
}

func TestCallerObservationRepairsAnUnreadablePeerRow(t *testing.T) {
	r, engine, _ := openCallerObservationRoster(t)
	caller := internalSeed(t, "caller", "203.0.113.1")
	r.ObserveCaller(t.Context(), caller, yagomodel.PeerSenior)
	corruptPeerRecord(t, r, engine, caller.Hash)

	if updates := observeCallerCountingUpdates(
		t, r, engine, caller, yagomodel.PeerSenior,
	); updates != 1 {
		t.Fatalf("observation over an unreadable row updates = %d, want 1", updates)
	}
	if r.KnownPeerCount(t.Context()) != 1 {
		t.Fatal("unreadable peer row was not repaired")
	}
}

func TestAnsweredCallerObservationWritesOnlyANewAnsweredEndpoint(t *testing.T) {
	r, engine, _ := openCallerObservationRoster(t)
	caller := internalSeed(t, "caller", "203.0.113.1")
	answered := answeredOrigin(t, "http://203.0.113.1:8090")
	r.ObserveCaller(t.Context(), caller, yagomodel.PeerSenior)

	before := engine.updates.Load()
	r.ObserveAnsweredCaller(t.Context(), caller, yagomodel.PeerSenior, answered)
	if updates := engine.updates.Load() - before; updates != 1 {
		t.Fatalf("new answered endpoint updates = %d, want 1", updates)
	}
	assertAnsweredEndpoint(t, r, caller.Hash, answered)

	before = engine.updates.Load()
	r.ObserveAnsweredCaller(t.Context(), caller, yagomodel.PeerSenior, answered)
	if updates := engine.updates.Load() - before; updates != 0 {
		t.Fatalf("repeated answered endpoint updates = %d, want 0", updates)
	}
}

func TestCallerObservationStoresNothingWhenThePreviousRowCannotBeRead(t *testing.T) {
	r, engine, _ := openCallerObservationRoster(t)
	caller := internalSeed(t, "caller", "203.0.113.1")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	before := engine.updates.Load()

	stored, err := r.persistCallerObservation(
		canceled,
		verifiedRosterEntry(caller, r.now()),
		yagomodel.None[peerwire.Endpoint](),
	)

	if stored || !errors.Is(err, context.Canceled) {
		t.Fatalf("persist over an unreadable vault = %t / %v, want refusal", stored, err)
	}
	if engine.updates.Load() != before || r.KnownPeerCount(t.Context()) != 0 {
		t.Fatal("unreadable previous observation still wrote the peer")
	}
}
