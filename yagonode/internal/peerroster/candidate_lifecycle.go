package peerroster

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const peerExpiryPruneLimit = 64

func (r *roster) detachEligibleCandidates(
	entries []rosterEntry,
	limit int,
) []yagomodel.Seed {
	limit = min(max(limit, 0), len(entries))
	now := r.now()
	seeds := make([]yagomodel.Seed, 0, limit)
	for _, entry := range entries {
		if len(seeds) == limit {
			break
		}
		if !entry.retryAfter.IsZero() && now.Before(entry.retryAfter) {
			continue
		}
		if !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
			continue
		}
		seeds = append(seeds, detachCandidateSeed(entry.seed))
	}
	return seeds
}

func (r *roster) PruneExpired(ctx context.Context) {
	_, active := r.activeSnapshot()
	candidates := r.selectInactive(
		ctx,
		active,
		peerExpiryPruneLimit,
		func(left, right rosterEntry) bool {
			return left.expiresAt.Before(right.expiresAt)
		},
	)
	now := r.now()
	expired := make([]yagomodel.Hash, 0, len(candidates))
	for _, entry := range candidates {
		if !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
			expired = append(expired, entry.seed.Hash)
		}
	}
	if len(expired) > 0 {
		r.evictExpiredPassive(ctx, expired)
	}
}

func (r *roster) evictExpiredPassive(ctx context.Context, peers []yagomodel.Hash) {
	if !r.acquireMembership(ctx) {
		return
	}
	defer r.releaseMembership()

	deleted, err := r.deleteExpiredPassive(ctx, peers)
	if err != nil {
		slog.WarnContext(ctx, "expired passive peer eviction failed", slog.Any("error", err))

		return
	}
	if len(deleted) > 0 {
		if err := r.rebuildEndpointOwnership(ctx); err != nil && context.Cause(ctx) == nil {
			slog.WarnContext(ctx, "peer endpoint ownership rebuild failed", slog.Any("error", err))
		}
		r.invalidateCandidateSnapshot()
	}
}

func (r *roster) deleteExpiredPassive(
	ctx context.Context,
	peers []yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	deleted := make([]yagomodel.Hash, 0, len(peers))
	now := r.now()
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, peer := range peers {
			if r.expiredPeerRemainsActive(peer, now) {
				continue
			}
			removed, err := r.deleteRosterEntry(tx, r.key(peer))
			if err != nil {
				return fmt.Errorf("delete expired passive peer: %w", err)
			}
			if removed {
				deleted = append(deleted, peer)
			}
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("delete expired passive peers: %w", err)
	}

	return deleted, nil
}

func (r *roster) expiredPeerRemainsActive(peer yagomodel.Hash, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, active := r.active[peer]
	if active && !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
		delete(r.active, peer)

		return false
	}

	return active
}
