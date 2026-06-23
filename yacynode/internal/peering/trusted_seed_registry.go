package peering

import (
	"context"
	"log/slog"
	"sync"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type trustedSeedRegistry struct {
	capacity int
	mu       sync.RWMutex
	seeds    map[yacymodel.Hash]yacymodel.Seed
}

func newTrustedSeedRegistry(capacity int) *trustedSeedRegistry {
	return &trustedSeedRegistry{
		capacity: capacity,
		seeds:    make(map[yacymodel.Hash]yacymodel.Seed),
	}
}

func (r *trustedSeedRegistry) Absorb(ctx context.Context, seeds ...yacymodel.Seed) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, seed := range seeds {
		hash := seed.Hash
		if _, known := r.seeds[hash]; !known && len(r.seeds) >= r.capacity {
			slog.WarnContext(
				ctx,
				"trusted seed discarded",
				slog.String("reason", "registry at capacity"),
				slog.Int("capacity", r.capacity),
			)

			continue
		}
		r.seeds[hash] = seed
	}
}

func (r *trustedSeedRegistry) Trusted(_ context.Context) []yacymodel.Seed {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]yacymodel.Seed, 0, len(r.seeds))
	for _, seed := range r.seeds {
		out = append(out, seed)
	}

	return out
}

var _ trustedSeedSource = (*trustedSeedRegistry)(nil)
