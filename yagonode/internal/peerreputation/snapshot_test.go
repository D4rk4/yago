package peerreputation

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
)

func TestSnapshotJSONRoundTripAndTransactionalDecode(t *testing.T) {
	t.Parallel()
	peer := validSnapshotPeer("peer-b", "group-b")
	other := validSnapshotPeer("peer-a", "group-a")
	snapshot, err := newSnapshot(200, 0.5, []PeerReputation{peer, other})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Snapshot
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	peersMatch := slices.Equal(snapshot.Peers(), decoded.Peers())
	timesMatch := snapshot.GeneratedAt().Equal(decoded.GeneratedAt())
	if !peersMatch || !timesMatch {
		t.Fatalf("round trip mismatch: %s", encoded)
	}
	before, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"version":2}`), &decoded); err == nil {
		t.Fatal("accepted unsupported snapshot")
	}
	after, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed snapshot decode mutated destination")
	}
	if err := json.Unmarshal([]byte(`{`), &decoded); err == nil {
		t.Fatal("accepted malformed snapshot")
	}
	if err := decoded.UnmarshalJSON([]byte(`{`)); err == nil {
		t.Fatal("accepted directly malformed snapshot")
	}
	invalidPayload := []byte(`{"version":1,"generated_at_unix_nano":200}`)
	if err := json.Unmarshal(invalidPayload, &decoded); err == nil {
		t.Fatal("accepted invalid snapshot payload")
	}
	var destination *Snapshot
	if err := destination.UnmarshalJSON(encoded); err == nil {
		t.Fatal("accepted nil snapshot destination")
	}
	if _, err := json.Marshal(Snapshot{}); err == nil {
		t.Fatal("encoded invalid zero snapshot")
	}
	if got := (Snapshot{}).Peer("newcomer"); got.Reliability != 0.5 || got.FusionWeight != 1 {
		t.Fatalf("zero snapshot newcomer is not neutral: %+v", got)
	}
}

func TestSnapshotValidation(t *testing.T) {
	t.Parallel()
	valid := validSnapshotPeer("peer", "group")
	if _, err := newSnapshot(0, 0.5, []PeerReputation{valid}); err == nil {
		t.Fatal("accepted invalid snapshot time")
	}
	if _, err := newSnapshot(200, 0, []PeerReputation{valid}); err == nil {
		t.Fatal("accepted zero neutral reliability")
	}
	if _, err := newSnapshot(200, 0.6, []PeerReputation{valid}); err == nil {
		t.Fatal("accepted optimistic neutral reliability")
	}
	if _, err := newSnapshot(200, 0.5, []PeerReputation{valid, valid}); err == nil {
		t.Fatal("accepted duplicate peer")
	}
	for name, peer := range invalidSnapshotPeers(valid) {
		t.Run(name, func(t *testing.T) {
			if _, err := newSnapshot(200, 0.5, []PeerReputation{peer}); err == nil {
				t.Fatal("accepted invalid peer")
			}
		})
	}
}

func TestSnapshotIndexValidation(t *testing.T) {
	t.Parallel()
	valid := validSnapshotPeer("peer", "group")
	validSnapshot, err := newSnapshot(200, 0.5, []PeerReputation{valid})
	if err != nil {
		t.Fatal(err)
	}
	invalidIndex := validSnapshot
	invalidIndex.byIdentity = map[SignedPeerIdentity]PeerReputation{}
	if err := validateSnapshot(invalidIndex); err == nil {
		t.Fatal("accepted missing snapshot index")
	}
	wrongIndex := validSnapshot
	wrong := valid
	wrong.Confidence = 0.9
	wrongIndex.byIdentity = map[SignedPeerIdentity]PeerReputation{"peer": wrong}
	if err := validateSnapshot(wrongIndex); err == nil {
		t.Fatal("accepted inconsistent snapshot index")
	}
	wrongOrder := validSnapshot
	wrongOrder.peers = []PeerReputation{
		validSnapshotPeer("z", "group"),
		validSnapshotPeer("a", "group"),
	}
	wrongOrder.byIdentity = map[SignedPeerIdentity]PeerReputation{
		"z": wrongOrder.peers[0],
		"a": wrongOrder.peers[1],
	}
	if err := validateSnapshot(wrongOrder); err == nil {
		t.Fatal("accepted inconsistent snapshot order")
	}
}

func invalidSnapshotPeers(valid PeerReputation) map[string]PeerReputation {
	peers := map[string]PeerReputation{}
	peers["unknown"] = changedPeer(valid, func(peer *PeerReputation) { peer.Known = false })
	peers["identity"] = changedPeer(valid, func(peer *PeerReputation) { peer.Peer = "" })
	peers["group"] = changedPeer(valid, func(peer *PeerReputation) { peer.NetworkGroup = "" })
	peers["reliability"] = changedPeer(valid, func(peer *PeerReputation) {
		peer.Reliability = math.NaN()
	})
	peers["fusion low"] = changedPeer(valid, func(peer *PeerReputation) { peer.FusionWeight = 0 })
	peers["fusion high"] = changedPeer(valid, func(peer *PeerReputation) { peer.FusionWeight = 2 })
	peers["confidence"] = changedPeer(valid, func(peer *PeerReputation) { peer.Confidence = -1 })
	peers["success evidence"] = changedPeer(valid, func(peer *PeerReputation) {
		peer.SuccessEvidence = math.Inf(1)
	})
	peers["failure evidence"] = changedPeer(valid, func(peer *PeerReputation) {
		peer.FailureEvidence = -1
	})
	peers["total evidence"] = changedPeer(valid, func(peer *PeerReputation) {
		peer.SuccessEvidence = maximumEvidence
		peer.FailureEvidence = 1
	})
	peers["observed zero"] = changedPeer(valid, func(peer *PeerReputation) {
		peer.LastObservedUnixNano = 0
	})
	peers["observed future"] = changedPeer(valid, func(peer *PeerReputation) {
		peer.LastObservedUnixNano = 201
	})

	return peers
}

func validSnapshotPeer(identity SignedPeerIdentity, group NetworkGroupKey) PeerReputation {
	return PeerReputation{
		Peer:                 identity,
		NetworkGroup:         group,
		Known:                true,
		Reliability:          0.6,
		FusionWeight:         1.2,
		Confidence:           0.5,
		SuccessEvidence:      2,
		FailureEvidence:      1,
		LastObservedUnixNano: 100,
	}
}

func changedPeer(peer PeerReputation, mutate func(*PeerReputation)) PeerReputation {
	mutate(&peer)

	return peer
}
