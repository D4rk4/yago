package peerroster

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

func (r *Roster) Discover(ctx context.Context, seeds ...yacymodel.Seed) {
	r.mu.Lock()
	defer r.mu.Unlock()

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

func (r *Roster) discover(ctx context.Context, seed yacymodel.Seed) error {
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		_, known, err := r.peers.Get(tx, r.key(seed.Hash))
		if err != nil {
			return fmt.Errorf("read peer: %w", err)
		}
		if known {
			return nil
		}
		if err := r.peers.Put(
			tx,
			r.key(seed.Hash),
			rosterEntry{seed: seed, lastSeen: r.now()},
		); err != nil {
			return fmt.Errorf("store peer: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("discover peer: %w", err)
	}

	return nil
}

func (r *Roster) evictOverflow(ctx context.Context) {
	victims := r.stalestBeyondCapacity(ctx)
	for _, hash := range victims {
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

// stalestBeyondCapacity returns the least-recently-seen inactive peers to trim
// once the reservoir grows past its capacity. A future recency index would
// replace this scan with a bounded range read.
func (r *Roster) stalestBeyondCapacity(ctx context.Context) []yacymodel.Hash {
	type candidate struct {
		hash     yacymodel.Hash
		lastSeen int64
	}

	var inactive []candidate
	total := 0
	if err := r.vault.View(ctx, func(tx *vault.Txn) error {
		if err := r.peers.Scan(tx, nil, func(_ vault.Key, entry rosterEntry) (bool, error) {
			total++
			if _, active := r.active[entry.seed.Hash]; !active {
				inactive = append(inactive, candidate{entry.seed.Hash, entry.lastSeen.UnixNano()})
			}

			return true, nil
		}); err != nil {
			return fmt.Errorf("scan peers: %w", err)
		}

		return nil
	}); err != nil {
		slog.WarnContext(ctx, "peer roster scan failed", slog.Any("error", err))

		return nil
	}

	excess := total - r.reservoirCap
	if excess <= 0 {
		return nil
	}

	sort.Slice(inactive, func(i, j int) bool {
		return inactive[i].lastSeen < inactive[j].lastSeen
	})

	victims := make([]yacymodel.Hash, 0, excess)
	for i := 0; i < excess && i < len(inactive); i++ {
		victims = append(victims, inactive[i].hash)
	}

	return victims
}
