package seedlist

import (
	"context"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagoproto"
)

func (e endpoint) xmlSeeds(
	ctx context.Context,
	req yagoproto.SeedlistRequest,
) []yagomodel.Seed {
	seeds := e.selectSeeds(ctx, req)
	if req.OwnSeedOnly {
		return seeds
	}

	return filterByName(seeds, req.PeerName, req.PeerNamePresent)
}

func (e endpoint) selectSeeds(
	ctx context.Context,
	req yagoproto.SeedlistRequest,
) []yagomodel.Seed {
	self := e.status.SelfSeed(ctx)
	if req.OwnSeedOnly {
		return []yagomodel.Seed{self}
	}
	if id, ok := req.ID.Get(); req.IDPresent || ok {
		if !ok {
			return nil
		}
		if id == self.Hash {
			return []yagomodel.Seed{self}
		}
		peer, found := e.peers.PeerByHash(ctx, id)
		if !found {
			return nil
		}

		return []yagomodel.Seed{peer}
	}
	if req.NamePresent || req.Name != "" {
		name := strings.TrimSuffix(req.Name, ".yacy")
		if name == "localpeer" {
			return []yagomodel.Seed{self}
		}
		peer, found := e.peers.PeerByName(ctx, name)
		if found {
			return []yagomodel.Seed{peer}
		}
		if selfSeedNameMatches(self, name) {
			return []yagomodel.Seed{self}
		}

		return nil
	}

	return e.regularSeeds(ctx, req, self)
}

func selfSeedNameMatches(seed yagomodel.Seed, name string) bool {
	seedName, known := seed.Name.Get()

	return known && strings.EqualFold(seedName, name)
}

func (e endpoint) regularSeeds(
	ctx context.Context,
	req yagoproto.SeedlistRequest,
	self yagomodel.Seed,
) []yagomodel.Seed {
	limit := seedlistMaxEntries
	if requested, ok := req.MaxCount.Get(); ok {
		limit = min(requested, seedlistMaxEntries)
	}
	seeds := make([]yagomodel.Seed, 0, max(limit, 1))
	if req.IncludeSelf {
		seeds = append(seeds, self)
	}
	active := make(map[yagomodel.Hash]struct{})
	for _, peer := range e.peers.ReachablePeers(ctx) {
		active[peer.Hash] = struct{}{}
	}
	floor, versionFloorKnown := req.MinVersion.Get()
	if !versionFloorKnown {
		floor = 0
	}
	for _, peer := range e.peers.SeedlistPeers(ctx, seedlistMaxEntries) {
		if len(seeds) >= limit {
			break
		}
		if req.NodeOnly && !rootNode(peer) {
			continue
		}
		if _, reachable := active[peer.Hash]; reachable &&
			!seedPassesVersionFloor(peer, floor) {
			continue
		}
		seeds = append(seeds, peer)
	}

	return seeds
}

func rootNode(seed yagomodel.Seed) bool {
	flags, known := seed.Flags.Get()

	return known && flags.Get(yagomodel.FlagRootNode)
}
