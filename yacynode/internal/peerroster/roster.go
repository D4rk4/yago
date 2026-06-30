package peerroster

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

const peersBucket vault.Name = "peerroster"

type roster struct {
	vault        *vault.Vault
	peers        *vault.Collection[rosterEntry]
	now          func() time.Time
	reservoirCap int
	activeCap    int

	mu     sync.Mutex
	active map[yacymodel.Hash]yacymodel.Seed
}

func (r *roster) key(hash yacymodel.Hash) vault.Key {
	return vault.Key(hash.String())
}

func (r *roster) Discover(ctx context.Context, seeds ...yacymodel.Seed) {
	for _, seed := range seeds {
		if _, reachable := seed.NetworkAddress(); !reachable {
			continue
		}
		if err := r.discover(ctx, seed); err != nil {
			slog.WarnContext(
				ctx,
				"peer discovery discarded",
				slog.String("peer", seed.Hash.String()),
				slog.Any("error", err),
			)
		}
	}

	r.evictOverflow(ctx)
}

func (r *roster) discover(ctx context.Context, seed yacymodel.Seed) error {
	key := r.key(seed.Hash)
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		_, known, err := r.peers.Get(tx, key)
		if err != nil {
			return fmt.Errorf("read peer: %w", err)
		}
		if known {
			return nil
		}
		if err := r.peers.Put(tx, key, rosterEntry{seed: seed, lastSeen: r.now()}); err != nil {
			return fmt.Errorf("store peer: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("discover peer: %w", err)
	}

	return nil
}

func (r *roster) ConfirmReachable(ctx context.Context, peer yacymodel.Hash) {
	confirmed, found := r.touch(ctx, peer)
	if !found {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, active := r.active[peer]; active || len(r.active) < r.activeCap {
		r.active[peer] = confirmed
	}
}

func (r *roster) touch(ctx context.Context, peer yacymodel.Hash) (yacymodel.Seed, bool) {
	var confirmed yacymodel.Seed
	found := false
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		entry, known, err := r.peers.Get(tx, r.key(peer))
		if err != nil {
			return fmt.Errorf("read peer: %w", err)
		}
		if !known {
			return nil
		}

		entry.lastSeen = r.now()
		if err := r.peers.Put(tx, r.key(peer), entry); err != nil {
			return fmt.Errorf("store peer: %w", err)
		}
		confirmed, found = entry.seed, true

		return nil
	}); err != nil {
		slog.WarnContext(
			ctx,
			"peer reachability not recorded",
			slog.String("peer", peer.String()),
			slog.Any("error", err),
		)

		return yacymodel.Seed{}, false
	}

	return confirmed, found
}

// Future: tolerate a bounded number of strikes with a cooldown before removal.
func (r *roster) ConfirmUnreachable(ctx context.Context, peer yacymodel.Hash) {
	r.mu.Lock()
	delete(r.active, peer)
	r.mu.Unlock()

	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := r.peers.Delete(tx, r.key(peer)); err != nil {
			return fmt.Errorf("delete peer: %w", err)
		}

		return nil
	}); err != nil {
		slog.WarnContext(
			ctx,
			"peer removal failed",
			slog.String("peer", peer.String()),
			slog.Any("error", err),
		)
	}
}

func (r *roster) ReachablePeers(_ context.Context) []yacymodel.Seed {
	r.mu.Lock()
	defer r.mu.Unlock()

	peers := make([]yacymodel.Seed, 0, len(r.active))
	for _, seed := range r.active {
		peers = append(peers, seed)
	}

	return peers
}

// Future: a recency index would replace this scan with a bounded range read.
func (r *roster) FreshestPeers(ctx context.Context, limit int) []yacymodel.Seed {
	targets, active := r.activeSnapshot()

	need := limit - len(targets)
	if need <= 0 {
		if len(targets) > limit {
			targets = targets[:limit]
		}

		return targets
	}

	fresh, _ := r.inactiveByFreshness(ctx, active)
	for i := 0; i < need && i < len(fresh); i++ {
		targets = append(targets, fresh[i].seed)
	}

	return targets
}

func (r *roster) activeSnapshot() ([]yacymodel.Seed, map[yacymodel.Hash]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	seeds := make([]yacymodel.Seed, 0, len(r.active))
	keys := make(map[yacymodel.Hash]struct{}, len(r.active))
	for hash, seed := range r.active {
		seeds = append(seeds, seed)
		keys[hash] = struct{}{}
	}

	return seeds, keys
}

func (r *roster) evictOverflow(ctx context.Context) {
	for _, hash := range r.stalestBeyondCapacity(ctx) {
		if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
			if _, err := r.peers.Delete(tx, r.key(hash)); err != nil {
				return fmt.Errorf("delete peer: %w", err)
			}

			return nil
		}); err != nil {
			slog.WarnContext(
				ctx,
				"peer eviction failed",
				slog.String("peer", hash.String()),
				slog.Any("error", err),
			)
		}
	}
}

func (r *roster) stalestBeyondCapacity(ctx context.Context) []yacymodel.Hash {
	_, active := r.activeSnapshot()
	fresh, total := r.inactiveByFreshness(ctx, active)

	excess := total - r.reservoirCap
	if excess <= 0 {
		return nil
	}

	victims := make([]yacymodel.Hash, 0, excess)
	for i := 0; i < excess && i < len(fresh); i++ {
		victims = append(victims, fresh[len(fresh)-1-i].seed.Hash)
	}

	return victims
}

func (r *roster) inactiveByFreshness(
	ctx context.Context,
	active map[yacymodel.Hash]struct{},
) ([]rosterEntry, int) {
	var fresh []rosterEntry
	total := 0
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		return r.peers.Scan(tx, nil, func(_ vault.Key, entry rosterEntry) (bool, error) {
			total++
			if _, ok := active[entry.seed.Hash]; !ok {
				fresh = append(fresh, entry)
			}

			return true, nil
		})
	}); err != nil {
		slog.WarnContext(ctx, "peer roster scan failed", slog.Any("error", err))

		return nil, 0
	}

	slices.SortFunc(fresh, func(a, b rosterEntry) int {
		return b.lastSeen.Compare(a.lastSeen)
	})

	return fresh, total
}
