package peerroster

import (
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func TestObservePotentialStoresVirginWithoutReachability(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	peer := internalSeed(t, "potential", "203.0.113.2")
	peer.PeerType = yagomodel.Some(yagomodel.PeerSenior)

	r.ObservePotential(t.Context(), peer)

	stored, found := r.PeerByHash(t.Context(), peer.Hash)
	classification, classified := stored.PeerType.Get()
	freshest := r.FreshestPeers(t.Context(), 1)
	if !found || !classified || classification != yagomodel.PeerVirgin ||
		r.ReachablePeerCount(t.Context()) != 0 ||
		len(freshest) != 0 {
		t.Fatalf(
			"stored = %#v found = %t reachable = %d freshest = %#v",
			stored,
			found,
			r.ReachablePeerCount(t.Context()),
			freshest,
		)
	}
}

func TestObservePotentialNeverOverwritesKnownPeer(t *testing.T) {
	r, _ := openScriptedRoster(t, 8, 4)
	known := internalSeed(t, "known", "203.0.113.3")
	known.PeerType = yagomodel.Some(yagomodel.PeerSenior)
	r.ObserveResponder(t.Context(), known)
	replacement := internalSeed(t, "known", "203.0.113.4")

	r.ObservePotential(t.Context(), replacement)

	stored, found := r.PeerByHash(t.Context(), known.Hash)
	address, _ := stored.NetworkAddress()
	classification, _ := stored.PeerType.Get()
	if !found || address != "203.0.113.3:8090" ||
		classification != yagomodel.PeerSenior || r.ReachablePeerCount(t.Context()) != 1 {
		t.Fatalf("stored = %#v reachable = %d", stored, r.ReachablePeerCount(t.Context()))
	}
}

func TestObservePotentialRejectsSelfAddresslessAndWriteFailure(t *testing.T) {
	r, engine := openScriptedRoster(t, 8, 4)
	self := internalSeed(t, "local", "203.0.113.1")
	r.ObservePotential(t.Context(), self)
	r.ObservePotential(t.Context(), yagomodel.Seed{Hash: internalHashFor("addressless")})
	engine.putErrors[peersBucket] = errors.New("put failed")
	r.ObservePotential(t.Context(), internalSeed(t, "failed", "203.0.113.2"))
	if r.KnownPeerCount(t.Context()) != 0 {
		t.Fatalf("known peers = %d, want 0", r.KnownPeerCount(t.Context()))
	}
}
