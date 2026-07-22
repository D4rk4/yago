package peerroster

import (
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
)

func TestDirectoryHonorsZeroAndOnePeerLimitsAndEmptyNames(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	first := internalSeed(t, "first", "203.0.113.1")
	first.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	first.Name = yagomodel.Some("First")
	second := internalSeed(t, "second", "203.0.113.2")
	second.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	second.Name = yagomodel.Some("Second")
	r.ObserveResponder(t.Context(), first)
	r.ObserveResponder(t.Context(), second)

	if peers := r.SeedlistPeers(t.Context(), 0); peers != nil {
		t.Fatalf("zero-limit seedlist peers = %#v", peers)
	}
	if peers := r.SeedlistPeers(t.Context(), 1); len(peers) != 1 {
		t.Fatalf("one-peer seedlist = %#v", peers)
	}
	if peer, found := r.PeerByName(t.Context(), ""); found || peer.Hash != "" {
		t.Fatalf("empty-name peer = %#v/%t", peer, found)
	}
}

func TestResponderRejectedByVerifiedEndpointOwnerIsNotStored(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	owner := internalSeed(t, "owner", "203.0.113.1")
	owner.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	claim := internalSeed(t, "claim", "203.0.113.1")
	claim.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.ObserveResponder(t.Context(), owner)

	r.ObserveResponder(t.Context(), claim)

	if _, found := r.PeerByHash(t.Context(), claim.Hash); found {
		t.Fatal("rejected verified endpoint claim was stored")
	}
}

func TestReachabilityConfirmationRejectsAConflictingVerifiedOwner(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "peer", "203.0.113.1")
	r.Discover(t.Context(), peer)
	endpoint := advertisedPeerEndpoints(peer)[0]
	r.endpointOwners[endpoint] = endpointOwnership{
		peer:     internalHashFor("owner"),
		verified: true,
	}

	r.ConfirmReachable(t.Context(), peer.Hash)

	if r.ReachablePeerCount(t.Context()) != 0 {
		t.Fatal("conflicting peer became reachable")
	}
}

func TestFreshestActiveSnapshotSortsByObservationTime(t *testing.T) {
	now := time.Unix(100, 0)
	r, _ := openScriptedRoster(t, 8, 4)
	r.now = func() time.Time { return now }
	first := internalSeed(t, "first", "203.0.113.1")
	second := internalSeed(t, "second", "203.0.113.2")
	r.ObserveResponder(t.Context(), first)
	now = now.Add(time.Second)
	r.ObserveResponder(t.Context(), second)

	peers := r.FreshestPeers(t.Context(), 2)

	if len(peers) != 2 || peers[0].Hash != second.Hash || peers[1].Hash != first.Hash {
		t.Fatalf("freshest active peers = %#v", peers)
	}
}
