package yagonode

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

type nodeSeedlistDirectory struct {
	roster    peerroster.Roster
	directory peerroster.Directory
}

func newNodeSeedlistDirectory(roster peerroster.Roster) nodeSeedlistDirectory {
	return nodeSeedlistDirectory{roster: roster, directory: roster.(peerroster.Directory)}
}

func (d nodeSeedlistDirectory) ReachablePeers(ctx context.Context) []yagomodel.Seed {
	return d.roster.ReachablePeers(ctx)
}

func (d nodeSeedlistDirectory) SeedlistPeers(
	ctx context.Context,
	limit int,
) []yagomodel.Seed {
	return d.directory.SeedlistPeers(ctx, limit)
}

func (d nodeSeedlistDirectory) PeerByHash(
	ctx context.Context,
	peer yagomodel.Hash,
) (yagomodel.Seed, bool) {
	return d.roster.PeerByHash(ctx, peer)
}

func (d nodeSeedlistDirectory) PeerByName(
	ctx context.Context,
	name string,
) (yagomodel.Seed, bool) {
	return d.directory.PeerByName(ctx, name)
}

func (r observedPeerRoster) SeedlistPeers(
	ctx context.Context,
	limit int,
) []yagomodel.Seed {
	return r.directory.SeedlistPeers(ctx, limit)
}

func (r observedPeerRoster) PeerByName(
	ctx context.Context,
	name string,
) (yagomodel.Seed, bool) {
	return r.directory.PeerByName(ctx, name)
}

func (r blockingRoster) SeedlistPeers(
	ctx context.Context,
	limit int,
) []yagomodel.Seed {
	peers := r.directory.SeedlistPeers(ctx, limit)
	blocked, err := r.blocks.Blocked(ctx)
	if err != nil {
		slog.WarnContext(ctx, peerBlockFanoutReadFailedMessage, slog.Any("error", err))

		return peers
	}
	excluded := make(map[yagomodel.Hash]struct{}, len(blocked))
	for _, entry := range blocked {
		excluded[entry.Hash] = struct{}{}
	}
	filtered := make([]yagomodel.Seed, 0, len(peers))
	for _, peer := range peers {
		if _, found := excluded[peer.Hash]; !found {
			filtered = append(filtered, peer)
		}
	}

	return filtered
}

func (r blockingRoster) PeerByName(
	ctx context.Context,
	name string,
) (yagomodel.Seed, bool) {
	peer, found := r.directory.PeerByName(ctx, name)

	return r.visiblePeer(ctx, peer, found)
}
