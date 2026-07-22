package peerroster

import (
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOpenReportsLifecycleRegistrationFailure(t *testing.T) {
	engine := newScriptedEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.RegisterKeyspace(
		storage,
		peerLifecyclesBucket,
		rosterLifecycleCodec{},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(
		t.Context(),
		storage,
		internalHashFor("local"),
		time.Now,
		Capacity{Reservoir: 8, Active: 4},
	); err == nil {
		t.Fatal("duplicate lifecycle registration was accepted")
	}
}

func TestOpenReportsLifecycleCleanupCursorRegistrationFailure(t *testing.T) {
	engine := newScriptedEngine()
	engine.provisionErrors[peerLifecycleCleanupCursorBucket] = errors.New("cursor provision failed")
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(
		t.Context(),
		storage,
		internalHashFor("local"),
		time.Now,
		Capacity{Reservoir: 8, Active: 4},
	); err == nil {
		t.Fatal("cleanup cursor registration failure was hidden")
	}
}

func TestOpenReportsLifecycleCleanupFailure(t *testing.T) {
	engine := newScriptedEngine()
	engine.keyPageError = errors.New("page failed")
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(
		t.Context(),
		storage,
		internalHashFor("local"),
		time.Now,
		Capacity{Reservoir: 8, Active: 4},
	); err == nil {
		t.Fatal("lifecycle cleanup failure was hidden")
	}
}

func TestMalformedRosterLifecycleFallsBackConservatively(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	r.Discover(t.Context(), peer)
	key := string(r.key(peer.Hash))
	engine.buckets[peerLifecyclesBucket][key] = []byte("bad")

	if _, found, err := r.PeerObservation(t.Context(), peer.Hash); err != nil || !found {
		t.Fatalf("point lookup fallback = %t/%v", found, err)
	}
	if observations, _, _, err := r.PeerObservations(t.Context()); err != nil ||
		len(observations) != 1 {
		t.Fatalf("roster scan fallback = %d/%v", len(observations), err)
	}
	if err := r.vault.View(t.Context(), func(tx *vault.Txn) error {
		entry, found, err := r.getRosterEntry(tx, r.key(peer.Hash))
		if err != nil {
			return err
		}
		if !found || entry.verified || !entry.retryAfter.IsZero() {
			t.Fatalf("malformed lifecycle fallback = %#v/%t", entry, found)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRosterLifecycleMutationFailuresRemainVisible(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	entry := rosterEntry{seed: peer, lastSeen: time.Unix(100, 0)}
	invalid := rosterEntry{seed: internalSeed(t, "peer", "203.0.113.1")}
	invalid.seed.Hash = "bad"
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return r.putRosterEntry(tx, r.key(invalid.seed.Hash), invalid)
	}); err == nil {
		t.Fatal("invalid peer row reached lifecycle storage")
	}

	engine.putErrors[peerLifecyclesBucket] = errors.New("lifecycle put failed")
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return r.putRosterEntry(tx, r.key(peer.Hash), entry)
	}); err == nil {
		t.Fatal("lifecycle put failure was hidden")
	}
	engine.putErrors[peerLifecyclesBucket] = nil
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return r.putRosterEntry(tx, r.key(peer.Hash), entry)
	}); err != nil {
		t.Fatal(err)
	}
	engine.deleteErrors[peerLifecyclesBucket] = errors.New("lifecycle delete failed")
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, err := r.deleteRosterEntry(tx, r.key(peer.Hash))

		return err
	}); err == nil {
		t.Fatal("lifecycle delete failure was hidden")
	}
}

func TestRosterLifecycleCleanupReportsPageFailure(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	engine.keyPageError = errors.New("page failed")
	if _, err := r.cleanupRosterLifecyclePage(t.Context(), nil, 1); err == nil {
		t.Fatal("lifecycle page failure was hidden")
	}
}

func TestRosterLifecycleCleanupReportsPeerFailure(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	r.Discover(t.Context(), peer)
	engine.buckets[peersBucket][string(r.key(peer.Hash))] = []byte("bad")
	if _, err := r.cleanupRosterLifecyclePage(t.Context(), nil, 1); err == nil {
		t.Fatal("lifecycle peer decode failure was hidden")
	}
}

func TestRosterLifecycleCleanupReportsOrphanDeleteFailure(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	lifecycle, err := rosterLifecycleFor(rosterEntry{
		seed: peer, lastSeen: time.Unix(100, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := (rosterLifecycleCodec{}).Encode(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[peerLifecyclesBucket][string(r.key(peer.Hash))] = raw
	engine.deleteErrors[peerLifecyclesBucket] = errors.New("delete failed")
	if _, err := r.cleanupRosterLifecyclePage(t.Context(), nil, 1); err == nil {
		t.Fatal("orphan lifecycle delete failure was hidden")
	}
}

func TestRosterLifecycleCleanupReportsMalformedDeleteFailure(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	engine.buckets[peerLifecyclesBucket]["malformed"] = []byte("bad")
	engine.deleteErrors[peerLifecyclesBucket] = errors.New("delete failed")
	if _, err := r.cleanupRosterLifecyclePage(t.Context(), nil, 1); err == nil {
		t.Fatal("malformed lifecycle delete failure was hidden")
	}
}

func TestRosterLifecycleCleanupReportsCursorStoreFailure(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	lifecycle, err := rosterLifecycleFor(rosterEntry{
		seed:     internalSeed(t, "peer", "203.0.113.1"),
		lastSeen: time.Unix(100, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := (rosterLifecycleCodec{}).Encode(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	engine.buckets[peerLifecyclesBucket]["first"] = append([]byte(nil), raw...)
	engine.buckets[peerLifecyclesBucket]["second"] = append([]byte(nil), raw...)
	engine.putErrors[peerLifecycleCleanupCursorBucket] = errors.New("put failed")
	if _, err := r.cleanupRosterLifecyclePage(t.Context(), nil, 1); err == nil {
		t.Fatal("cleanup cursor put failure was hidden")
	}
}

func TestRosterLifecycleCleanupReportsCursorClearFailure(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	entry := rosterEntry{seed: peer, lastSeen: time.Unix(100, 0)}
	if err := r.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return r.putRosterEntry(tx, r.key(peer.Hash), entry)
	}); err != nil {
		t.Fatal(err)
	}
	engine.buckets[peerLifecycleCleanupCursorBucket][string(
		rosterLifecycleCleanupCursorKey,
	)] = []byte("position")
	engine.deleteErrors[peerLifecycleCleanupCursorBucket] = errors.New("delete failed")
	if _, err := r.cleanupRosterLifecyclePage(t.Context(), nil, 1); err == nil {
		t.Fatal("cleanup cursor clear failure was hidden")
	}
}

func TestMalformedRosterLifecycleCleanupCursorIsDiscarded(t *testing.T) {
	_, engine := openScriptedRoster(t, 8, 4)
	key := string(rosterLifecycleCleanupCursorKey)
	engine.buckets[peerLifecycleCleanupCursorBucket][key] = make(
		[]byte,
		rosterLifecycleCleanupCursorMaximumWidth+1,
	)
	reopenScriptedRoster(t, engine, time.Now)
	if _, found := engine.buckets[peerLifecycleCleanupCursorBucket][key]; found {
		t.Fatal("malformed cleanup cursor survived roster open")
	}
}

func TestMalformedRosterLifecycleCleanupCursorDeleteFailureIsVisible(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	key := string(rosterLifecycleCleanupCursorKey)
	engine.buckets[peerLifecycleCleanupCursorBucket][key] = make(
		[]byte,
		rosterLifecycleCleanupCursorMaximumWidth+1,
	)
	engine.deleteErrors[peerLifecycleCleanupCursorBucket] = errors.New("delete failed")
	if err := r.cleanupRosterLifecycleOrphans(t.Context()); err == nil {
		t.Fatal("malformed cleanup cursor delete failure was hidden")
	}
}
