package peerreputation

import (
	"math"
	"testing"
)

func TestNetworkGroupInfluenceCap(t *testing.T) {
	t.Parallel()
	snapshot, err := newSnapshot(200, 0.5, []PeerReputation{
		validSnapshotPeer("trusted", "stored-a"),
		mutateReputationWeight(validSnapshotPeer("distrusted", "stored-b"), 0.5),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := snapshot.CapNetworkGroupInfluence([]PeerInfluence{
		{Peer: "unknown", NetworkGroup: "group-b", BaseWeight: 0.5},
		{Peer: "trusted", NetworkGroup: "group-a", BaseWeight: 1},
		{Peer: "distrusted", NetworkGroup: "group-a", BaseWeight: 2},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	ordered := result[0].Peer == "distrusted" &&
		result[1].Peer == "trusted" &&
		result[2].Peer == "unknown"
	if !ordered {
		t.Fatalf("result is not deterministic: %+v", result)
	}
	if result[0].ReputationWeight != 0.5 || result[1].ReputationWeight != 1.2 ||
		result[2].ReputationWeight != 1 {
		t.Fatalf("unexpected reputation weights: %+v", result)
	}
	groupA := result[0].Weight + result[1].Weight
	if groupA > 1 || math.Abs(groupA-1) > 1e-12 || result[2].Weight != 0.5 {
		t.Fatalf("unexpected capped weights: %+v", result)
	}
	uncapped, err := snapshot.CapNetworkGroupInfluence([]PeerInfluence{{
		Peer: "unknown", NetworkGroup: "group", BaseWeight: 0,
	}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if uncapped[0].Weight != 0 {
		t.Fatalf("zero influence changed: %+v", uncapped)
	}
	if empty, err := snapshot.CapNetworkGroupInfluence(nil, 1); err != nil || len(empty) != 0 {
		t.Fatalf("unexpected empty cap result: %+v %v", empty, err)
	}
}

func TestNetworkGroupInfluenceValidation(t *testing.T) {
	t.Parallel()
	snapshot, err := newSnapshot(200, 0.5, nil)
	if err != nil {
		t.Fatal(err)
	}
	invalidCaps := []float64{0, math.NaN(), maximumInfluenceWeight + 1}
	for _, cap := range invalidCaps {
		if _, err := snapshot.CapNetworkGroupInfluence(nil, cap); err == nil {
			t.Fatalf("accepted cap %v", cap)
		}
	}
	invalidInfluences := [][]PeerInfluence{
		{{Peer: "", NetworkGroup: "group", BaseWeight: 1}},
		{{Peer: "peer", NetworkGroup: "", BaseWeight: 1}},
		{{Peer: "peer", NetworkGroup: "group", BaseWeight: math.NaN()}},
		{{Peer: "peer", NetworkGroup: "group", BaseWeight: -1}},
		{{Peer: "peer", NetworkGroup: "group", BaseWeight: maximumInfluenceWeight + 1}},
		{
			{Peer: "peer", NetworkGroup: "group-a", BaseWeight: 1},
			{Peer: "peer", NetworkGroup: "group-b", BaseWeight: 1},
		},
	}
	for _, influences := range invalidInfluences {
		if _, err := snapshot.CapNetworkGroupInfluence(influences, 1); err == nil {
			t.Fatalf("accepted influences %+v", influences)
		}
	}
}

func mutateReputationWeight(peer PeerReputation, weight float64) PeerReputation {
	peer.FusionWeight = weight

	return peer
}
