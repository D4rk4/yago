package peerroster

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOpenSurfacesRosterCapacityScanFailure(t *testing.T) {
	engine := newScriptedEngine()
	engine.scanErrors[peersBucket] = errors.New("scan failed")
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}

	if _, err := Open(
		t.Context(), storage, internalHashFor("local"), time.Now,
		Capacity{Reservoir: 8, Active: 4},
	); err == nil {
		t.Fatal("Open accepted a roster capacity scan failure")
	}
}

func TestInitializeRosterCapacitySurfacesDeletionFailure(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	r.Discover(
		t.Context(),
		internalSeed(t, "first", "203.0.113.1"),
		internalSeed(t, "second", "203.0.113.2"),
	)
	r.reservoirCap = 1
	engine.deleteErrors[peersBucket] = errors.New("delete failed")

	if err := r.initializeRosterCapacity(t.Context()); err == nil {
		t.Fatal("capacity initialization accepted a deletion failure")
	}
}

func TestTrimOverflowSurfacesCountAndRosterScanFailures(t *testing.T) {
	t.Run("count", func(t *testing.T) {
		r, engine := openScriptedRoster(t, 1, 1)
		corruptPeerCount(t, engine)

		if _, err := r.trimOverflow(t.Context()); err == nil {
			t.Fatal("overflow trim accepted a count failure")
		}
	})

	t.Run("roster scan", func(t *testing.T) {
		r, engine := openScriptedRoster(t, 8, 1)
		r.Discover(
			t.Context(),
			internalSeed(t, "first", "203.0.113.1"),
			internalSeed(t, "second", "203.0.113.2"),
		)
		r.reservoirCap = 1
		engine.scanErrors[peersBucket] = errors.New("scan failed")

		if _, err := r.trimOverflow(t.Context()); err == nil {
			t.Fatal("overflow trim accepted a roster scan failure")
		}
	})
}

func TestTrimOverflowPreservesEveryActivePeerBeyondReservoirCapacity(t *testing.T) {
	r, _ := openScriptedRoster(t, 1, 4)
	first := internalSeed(t, "first", "203.0.113.1")
	first.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	second := internalSeed(t, "second", "203.0.113.2")
	second.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.ObserveResponder(t.Context(), first)
	r.ObserveResponder(t.Context(), second)

	changed, err := r.trimOverflow(t.Context())

	if err != nil || changed || r.KnownPeerCount(t.Context()) != 2 {
		t.Fatalf("active overflow trim = changed %t known %d error %v",
			changed, r.KnownPeerCount(t.Context()), err)
	}
}

func TestRosterCapacitySelectionPrefersActivePeersOverPassivePeers(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 2)
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	for _, peer := range []yagomodel.Seed{first, second} {
		peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)
		r.ObserveResponder(t.Context(), peer)
	}
	r.Discover(
		t.Context(),
		internalSeed(t, "third", "203.0.113.3"),
		internalSeed(t, "fourth", "203.0.113.4"),
	)
	r.reservoirCap = 1

	retained, total, err := r.rosterEntriesWithinCapacity(t.Context())

	if err != nil || total != 4 || len(retained) != 2 {
		t.Fatalf("capacity selection = retained %d total %d error %v", len(retained), total, err)
	}
	for _, entry := range retained {
		if entry.seed.Hash != first.Hash && entry.seed.Hash != second.Hash {
			t.Fatalf("passive peer displaced active capacity: %#v", retained)
		}
	}
}

func TestRosterCapacityScanHonorsMidScanCancellation(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	r.Discover(t.Context(), internalSeed(t, "peer", "203.0.113.1"))
	ctx, cancel := context.WithCancel(t.Context())
	engine.scanObserver = func(vault.Name) { cancel() }

	if _, _, err := r.rosterEntriesWithinCapacity(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capacity scan error = %v", err)
	}
}

func TestRosterCapacityDeletionSurfacesKeyPageFailure(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	engine.keyPageError = errors.New("key page failed")

	if _, err := r.deleteRosterEntriesOutside(t.Context(), nil); err == nil {
		t.Fatal("capacity deletion accepted a key-page failure")
	}
}
