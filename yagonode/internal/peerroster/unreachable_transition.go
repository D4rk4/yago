package peerroster

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type unreachablePersistence struct {
	changed bool
	deleted bool
}

func (r *roster) ConfirmUnreachable(ctx context.Context, peer yagomodel.Hash) {
	if r.isSelf(peer) {
		return
	}
	if !r.acquireMembership(ctx) {
		return
	}
	defer r.releaseMembership()

	active := r.removeActiveMembership(peer)
	slog.DebugContext(
		ctx,
		"peer cooling after transport failure",
		slog.String("peer", peer.String()),
	)
	persistence, err := r.persistUnreachable(ctx, peer)
	if err != nil {
		slog.WarnContext(
			ctx,
			"peer removal failed",
			slog.String("peer", peer.String()),
			slog.Any("error", err),
		)
	}
	if persistence.deleted {
		r.rebuildEndpointOwnershipAfterDeletion(ctx)
	}
	if active || persistence.changed {
		r.invalidateCandidateSnapshot()
	}
}

func (r *roster) removeActiveMembership(peer yagomodel.Hash) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.active, r.self)
	_, active := r.active[peer]
	delete(r.active, peer)

	return active
}

func (r *roster) persistUnreachable(
	ctx context.Context,
	peer yagomodel.Hash,
) (unreachablePersistence, error) {
	persistence := unreachablePersistence{}
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		entry, known, err := r.getRosterEntry(tx, r.key(peer))
		if err != nil {
			return fmt.Errorf("read peer: %w", err)
		}
		if !known {
			return nil
		}
		now := r.now()
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			removed, err := r.deleteRosterEntry(tx, r.key(peer))
			if err != nil {
				return fmt.Errorf("delete stale peer: %w", err)
			}
			persistence.deleted = removed
			persistence.changed = removed

			return nil
		}
		entry.retryAfter = now.Add(peerPassiveRetryCooldown)
		if err := r.putRosterEntry(tx, r.key(peer), entry); err != nil {
			return fmt.Errorf("store peer cooldown: %w", err)
		}
		persistence.changed = true

		return nil
	}); err != nil {
		return unreachablePersistence{}, fmt.Errorf("persist unreachable peer: %w", err)
	}

	return persistence, nil
}

func (r *roster) rebuildEndpointOwnershipAfterDeletion(ctx context.Context) {
	if err := r.rebuildEndpointOwnership(ctx); err != nil && context.Cause(ctx) == nil {
		slog.WarnContext(ctx, "peer endpoint ownership rebuild failed", slog.Any("error", err))
	}
}
