package peerroster

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const peersBucket vault.Name = "peerroster"

const (
	peerRemoteIndexRejectedMessage        = "peer remote index rejected"
	peerRemoteIndexRejectionFailedMessage = "peer remote index rejection failed"
)

type roster struct {
	vault        *vault.Vault
	peers        *vault.Collection[rosterEntry]
	now          func() time.Time
	reservoirCap int
	activeCap    int

	mu     sync.Mutex
	active map[yagomodel.Hash]yagomodel.Seed
}

func (r *roster) key(hash yagomodel.Hash) vault.Key {
	return vault.Key(hash.String())
}

func (r *roster) Discover(ctx context.Context, seeds ...yagomodel.Seed) {
	for _, seed := range seeds {
		if _, reachable := seed.NetworkAddress(); !reachable {
			continue
		}
		if err := r.discoverOne(ctx, seed); err != nil {
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

func (r *roster) discoverOne(ctx context.Context, seed yagomodel.Seed) error {
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

func (r *roster) ConfirmReachable(ctx context.Context, peer yagomodel.Hash) {
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

func (r *roster) touch(ctx context.Context, peer yagomodel.Hash) (yagomodel.Seed, bool) {
	var confirmed yagomodel.Seed
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

		return yagomodel.Seed{}, false
	}

	return confirmed, found
}

// Future: tolerate a bounded number of strikes with a cooldown before removal.
func (r *roster) ConfirmUnreachable(ctx context.Context, peer yagomodel.Hash) {
	r.mu.Lock()
	delete(r.active, peer)
	r.mu.Unlock()

	slog.DebugContext(ctx, "peer dropped as unreachable", slog.String("peer", peer.String()))

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

func (r *roster) RejectRemoteIndex(ctx context.Context, failed yagomodel.Seed) {
	var updated yagomodel.Seed
	changed := false
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		entry, known, err := r.peers.Get(tx, r.key(failed.Hash))
		if err != nil {
			return fmt.Errorf("read peer: %w", err)
		}
		if !known || !entry.seed.SharesAddress(failed) {
			return nil
		}
		entry.seed = withoutRemoteIndex(entry.seed)
		if err := r.peers.Put(tx, r.key(failed.Hash), entry); err != nil {
			return fmt.Errorf("store peer: %w", err)
		}
		updated = entry.seed
		changed = true

		return nil
	}); err != nil {
		slog.WarnContext(
			ctx,
			peerRemoteIndexRejectionFailedMessage,
			slog.String("peer", failed.Hash.String()),
			slog.Any("error", err),
		)

		return
	}
	if !changed {
		return
	}

	r.mu.Lock()
	if _, active := r.active[failed.Hash]; active {
		r.active[failed.Hash] = updated
	}
	r.mu.Unlock()

	slog.WarnContext(ctx, peerRemoteIndexRejectedMessage, slog.String("peer", failed.Hash.String()))
}

func (r *roster) ReachablePeers(_ context.Context) []yagomodel.Seed {
	r.mu.Lock()
	defer r.mu.Unlock()

	peers := make([]yagomodel.Seed, 0, len(r.active))
	for _, seed := range r.active {
		peers = append(peers, seed)
	}

	return peers
}

func withoutRemoteIndex(seed yagomodel.Seed) yagomodel.Seed {
	flags, ok := seed.Flags.Get()
	if !ok {
		flags = yagomodel.ZeroFlags()
	}
	seed.Flags = yagomodel.Some(flags.Set(yagomodel.FlagAcceptRemoteIndex, false))

	return seed
}

func (r *roster) KnownPeerCount(ctx context.Context) int {
	return r.peerCount(ctx)
}

func (r *roster) ReachablePeerCount(_ context.Context) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.active)
}

// Future: a recency index would replace this scan with a bounded range read.
func (r *roster) FreshestPeers(ctx context.Context, limit int) []yagomodel.Seed {
	targets, active := r.activeSnapshot()

	need := limit - len(targets)
	if need <= 0 {
		if len(targets) > limit {
			targets = targets[:limit]
		}

		return targets
	}

	for _, entry := range r.freshestInactive(ctx, active, need) {
		targets = append(targets, entry.seed)
	}

	return targets
}

func (r *roster) activeSnapshot() ([]yagomodel.Seed, map[yagomodel.Hash]struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	seeds := make([]yagomodel.Seed, 0, len(r.active))
	keys := make(map[yagomodel.Hash]struct{}, len(r.active))
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

func (r *roster) stalestBeyondCapacity(ctx context.Context) []yagomodel.Hash {
	_, active := r.activeSnapshot()

	excess := r.peerCount(ctx) - r.reservoirCap
	if excess <= 0 {
		return nil
	}

	stale := r.stalestInactive(ctx, active, excess)
	victims := make([]yagomodel.Hash, 0, len(stale))
	for _, entry := range stale {
		victims = append(victims, entry.seed.Hash)
	}

	return victims
}

func (r *roster) freshestInactive(
	ctx context.Context,
	active map[yagomodel.Hash]struct{},
	limit int,
) []rosterEntry {
	return r.selectInactive(ctx, active, limit, func(a, b rosterEntry) bool {
		return a.lastSeen.Compare(b.lastSeen) > 0
	})
}

func (r *roster) stalestInactive(
	ctx context.Context,
	active map[yagomodel.Hash]struct{},
	limit int,
) []rosterEntry {
	return r.selectInactive(ctx, active, limit, func(a, b rosterEntry) bool {
		return a.lastSeen.Compare(b.lastSeen) < 0
	})
}

func (r *roster) selectInactive(
	ctx context.Context,
	active map[yagomodel.Hash]struct{},
	limit int,
	precedes func(a, b rosterEntry) bool,
) []rosterEntry {
	if limit <= 0 {
		return nil
	}

	kept := make([]rosterEntry, 0, limit)
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		return r.peers.Scan(tx, nil, func(_ vault.Key, entry rosterEntry) (bool, error) {
			if _, ok := active[entry.seed.Hash]; ok {
				return true, nil
			}

			pos := 0
			for pos < len(kept) && !precedes(entry, kept[pos]) {
				pos++
			}
			if pos >= limit {
				return true, nil
			}
			if len(kept) < limit {
				kept = append(kept, rosterEntry{})
			}
			copy(kept[pos+1:], kept[pos:])
			kept[pos] = entry

			return true, nil
		})
	}); err != nil {
		slog.WarnContext(ctx, "peer roster scan failed", slog.Any("error", err))

		return nil
	}

	return kept
}

func (r *roster) peerCount(ctx context.Context) int {
	total := 0
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		count, err := r.peers.Len(tx)
		if err != nil {
			return fmt.Errorf("count peers: %w", err)
		}
		total = count

		return nil
	}); err != nil {
		slog.WarnContext(ctx, "peer roster count failed", slog.Any("error", err))

		return 0
	}

	return total
}
