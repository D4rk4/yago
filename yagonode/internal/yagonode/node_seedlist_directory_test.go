package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

func (r reachableRoster) SeedlistPeers(_ context.Context, limit int) []yagomodel.Seed {
	if limit <= 0 {
		return nil
	}
	if len(r.peers) > limit {
		return r.peers[:limit]
	}

	return r.peers
}

func (r reachableRoster) PeerByName(
	_ context.Context,
	name string,
) (yagomodel.Seed, bool) {
	for _, peer := range r.peers {
		peerName, known := peer.Name.Get()
		if known && peerName == name {
			return peer, true
		}
	}

	return yagomodel.Seed{}, false
}

func TestNodeSeedlistDirectoryExcludesBlockedPeersFromEverySelector(t *testing.T) {
	blocked := yagomodel.Seed{
		Hash: yagomodel.Hash("AAAAAAAAAAAA"),
		Name: yagomodel.Some("blocked"),
	}
	allowed := yagomodel.Seed{
		Hash: yagomodel.Hash("BBBBBBBBBBBB"),
		Name: yagomodel.Some("allowed"),
	}
	base := reachableRoster{peers: []yagomodel.Seed{blocked, allowed}}
	observed := observePeerRoster(t.Context(), base, &recordingPeerMetrics{})
	directory := newNodeSeedlistDirectory(
		newBlockingRoster(observed, newFakePeerBlocks(blocked.Hash)),
	)

	reachable := directory.ReachablePeers(t.Context())
	if len(reachable) != 1 || reachable[0].Hash != allowed.Hash {
		t.Fatalf("reachable peers = %+v, want allowed peer", reachable)
	}
	listed := directory.SeedlistPeers(t.Context(), 2)
	if len(listed) != 1 || listed[0].Hash != allowed.Hash {
		t.Fatalf("seedlist peers = %+v, want allowed peer", listed)
	}
	if peer, found := directory.PeerByHash(t.Context(), blocked.Hash); found {
		t.Fatalf("blocked hash resolved to %+v", peer)
	}
	if peer, found := directory.PeerByName(t.Context(), "blocked"); found {
		t.Fatalf("blocked name resolved to %+v", peer)
	}
	if peer, found := directory.PeerByHash(
		t.Context(),
		allowed.Hash,
	); !found ||
		peer.Hash != allowed.Hash {
		t.Fatalf("allowed hash = %+v/%t", peer, found)
	}
	if peer, found := directory.PeerByName(
		t.Context(),
		"allowed",
	); !found ||
		peer.Hash != allowed.Hash {
		t.Fatalf("allowed name = %+v/%t", peer, found)
	}
	if peer, found := directory.PeerByName(t.Context(), "missing"); found {
		t.Fatalf("missing name resolved to %+v", peer)
	}
}

func TestNodeSeedlistDirectoryFailsOpenOnBlocklistReadErrors(t *testing.T) {
	peer := yagomodel.Seed{
		Hash: yagomodel.Hash("AAAAAAAAAAAA"),
		Name: yagomodel.Some("peer"),
	}
	blocks := newFakePeerBlocks()
	blocks.blockedErr = errors.New("list failed")
	directory := newNodeSeedlistDirectory(
		newBlockingRoster(reachableRoster{peers: []yagomodel.Seed{peer}}, blocks),
	)

	listed := directory.SeedlistPeers(t.Context(), 1)
	if len(listed) != 1 || listed[0].Hash != peer.Hash {
		t.Fatalf("seedlist peers after blocklist failure = %+v", listed)
	}
	blocks.blockedErr = nil
	blocks.isBlockedErr = errors.New("lookup failed")
	if resolved, found := directory.PeerByHash(
		t.Context(),
		peer.Hash,
	); !found ||
		resolved.Hash != peer.Hash {
		t.Fatalf("peer after blocklist failure = %+v/%t", resolved, found)
	}
}
