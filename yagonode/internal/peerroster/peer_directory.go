package peerroster

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
)

const seedlistPeerLimit = 1000

func (r *roster) SeedlistPeers(ctx context.Context, limit int) []yagomodel.Seed {
	limit = min(max(limit, 0), seedlistPeerLimit)
	if limit == 0 {
		return nil
	}
	now := r.now()
	entries := r.freshestCandidateEntries(ctx)
	if entries == nil {
		return nil
	}
	seeds := make([]yagomodel.Seed, 0, min(limit, len(entries)))
	for _, entry := range entries {
		if !entry.verified || !routingClassificationEligible(entry.seed) ||
			(!entry.expiresAt.IsZero() && !now.Before(entry.expiresAt)) {
			continue
		}
		seeds = append(seeds, detachCandidateSeed(entry.seed))
		if len(seeds) == limit {
			break
		}
	}

	return seeds
}

func (r *roster) PeerByName(ctx context.Context, name string) (yagomodel.Seed, bool) {
	if name == "" {
		return yagomodel.Seed{}, false
	}
	now := r.now()
	for _, entry := range r.freshestCandidateEntries(ctx) {
		if !entry.verified || !routingClassificationEligible(entry.seed) ||
			(!entry.expiresAt.IsZero() && !now.Before(entry.expiresAt)) {
			continue
		}
		peerName, known := entry.seed.Name.Get()
		if known && yagomodel.NormalizeSeedName(peerName) == yagomodel.NormalizeSeedName(name) {
			return detachCandidateSeed(entry.seed), true
		}
	}

	return yagomodel.Seed{}, false
}
