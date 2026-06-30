package peerroster

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

func (r *Roster) ReachablePeers(_ context.Context) []yacymodel.Seed {
	r.mu.Lock()
	defer r.mu.Unlock()

	peers := make([]yacymodel.Seed, 0, len(r.active))
	for _, seed := range r.active {
		peers = append(peers, seed)
	}

	return peers
}

// GreetTargets returns the peers the announcement loop should greet this round:
// the currently reachable set, topped up with the freshest candidates from the
// reservoir until the active capacity is reached. A future recency index would
// replace this scan with a bounded range read.
func (r *Roster) GreetTargets(ctx context.Context) []yacymodel.Seed {
	r.mu.Lock()
	defer r.mu.Unlock()

	targets := make([]yacymodel.Seed, 0, r.activeCap)
	for _, seed := range r.active {
		targets = append(targets, seed)
	}

	need := r.activeCap - len(targets)
	if need <= 0 {
		return targets
	}

	type candidate struct {
		seed     yacymodel.Seed
		lastSeen int64
	}

	var fresh []candidate
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		if err := r.peers.Scan(tx, nil, func(_ vault.Key, entry rosterEntry) (bool, error) {
			if _, active := r.active[entry.seed.Hash]; !active {
				fresh = append(fresh, candidate{entry.seed, entry.lastSeen.UnixNano()})
			}

			return true, nil
		}); err != nil {
			return fmt.Errorf("scan peers: %w", err)
		}

		return nil
	}); err != nil {
		slog.WarnContext(ctx, "peer roster scan failed", slog.Any("error", err))

		return targets
	}

	sort.Slice(fresh, func(i, j int) bool {
		return fresh[i].lastSeen > fresh[j].lastSeen
	})

	for i := 0; i < need && i < len(fresh); i++ {
		targets = append(targets, fresh[i].seed)
	}

	return targets
}
