package peerroster

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestCallerObservationHandlesPersistenceFailures(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	caller := internalSeed(t, "caller", "203.0.113.2")
	engine.putErrors[peersBucket] = errors.New("put failed")

	r.ObserveCaller(t.Context(), caller, yagomodel.PeerJunior)

	if r.KnownPeerCount(t.Context()) != 0 || r.ReachablePeerCount(t.Context()) != 0 {
		t.Fatal("failed caller observation changed roster")
	}

	engine.putErrors[peersBucket] = nil
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	r.ObserveCaller(canceled, caller, yagomodel.PeerSenior)
	if r.KnownPeerCount(t.Context()) != 0 || r.ReachablePeerCount(t.Context()) != 0 {
		t.Fatal("canceled caller observation changed roster")
	}
}

func TestCallerObservationHonorsActiveCapacityAndRefreshesActivePeer(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 1)
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")

	r.ObserveCaller(t.Context(), first, yagomodel.PeerSenior)
	r.ObserveCaller(t.Context(), second, yagomodel.PeerSenior)
	first.Name = yagomodel.Some("updated")
	r.ObserveCaller(t.Context(), first, yagomodel.PeerSenior)

	reachable := r.ReachablePeers(t.Context())
	name, _ := reachable[0].Name.Get()
	if len(reachable) != 1 || reachable[0].Hash != first.Hash || name != "updated" ||
		r.KnownPeerCount(t.Context()) != 2 {
		t.Fatalf("reachable callers = %#v, known %d", reachable, r.KnownPeerCount(t.Context()))
	}
}

func TestCallerObservationRetainsPrincipalClassification(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	caller := internalSeed(t, "principal", "203.0.113.3")

	r.ObserveCaller(t.Context(), caller, yagomodel.PeerPrincipal)

	reachable := r.ReachablePeers(t.Context())
	if len(reachable) != 1 || reachable[0].Hash != caller.Hash {
		t.Fatalf("reachable principal = %#v", reachable)
	}
	peerType, known := reachable[0].PeerType.Get()
	if !known || peerType != yagomodel.PeerPrincipal {
		t.Fatalf("principal classification = %q known=%t", peerType, known)
	}
}

func TestCandidateSnapshotRejectsPersistedAddresslessPeer(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	peer := yagomodel.Seed{
		Hash:     internalHashFor("legacy"),
		PeerType: yagomodel.Some(yagomodel.PeerJunior),
	}
	if _, err := r.persistCallerObservation(t.Context(), rosterEntry{seed: peer}); err != nil {
		t.Fatalf("persistCallerObservation: %v", err)
	}
	r.invalidateCandidateSnapshot()

	if candidates := r.FreshestPeers(t.Context(), 8); len(candidates) != 0 {
		t.Fatalf("addressless candidates = %#v, want none", candidates)
	}
	if r.KnownPeerCount(t.Context()) != 1 {
		t.Fatal("addressless persisted observation should remain visible to observation readers")
	}
}
