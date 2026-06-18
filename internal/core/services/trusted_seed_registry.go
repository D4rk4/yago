package services

import (
	"context"
	"log/slog"
	"sync"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type TrustedSeedRegistry struct {
	capacity int
	mu       sync.RWMutex
	seeds    map[yacymodel.Hash]yacymodel.Seed
}

func NewTrustedSeedRegistry(capacity int) *TrustedSeedRegistry {
	return &TrustedSeedRegistry{
		capacity: capacity,
		seeds:    make(map[yacymodel.Hash]yacymodel.Seed),
	}
}

func (r *TrustedSeedRegistry) Absorb(ctx context.Context, seeds ...yacymodel.Seed) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, seed := range seeds {
		hash, err := seed.Hash()
		if err != nil {
			slog.WarnContext(ctx, "trusted seed discarded", "error", err)

			continue
		}
		if _, known := r.seeds[hash]; !known && len(r.seeds) >= r.capacity {
			slog.WarnContext(
				ctx,
				"trusted seed discarded",
				"reason",
				"registry at capacity",
				"capacity",
				r.capacity,
			)

			continue
		}
		r.seeds[hash] = seed
	}
}

func (r *TrustedSeedRegistry) Trusted(_ context.Context) []yacymodel.Seed {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]yacymodel.Seed, 0, len(r.seeds))
	for _, seed := range r.seeds {
		out = append(out, seed)
	}

	return out
}

var _ trustedSeedSource = (*TrustedSeedRegistry)(nil)
