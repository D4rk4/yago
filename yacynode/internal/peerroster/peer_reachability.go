package peerroster

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

func (r *Roster) Reachable(ctx context.Context, peer yacymodel.Hash) {
	r.mu.Lock()
	defer r.mu.Unlock()

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

		return
	}
	if !found {
		return
	}

	if _, active := r.active[peer]; active || len(r.active) < r.activeCap {
		r.active[peer] = confirmed
	}
}

// Unreachable drops the peer on its first failed greet. A future refinement would
// tolerate a bounded number of strikes with a cooldown before removal.
func (r *Roster) Unreachable(ctx context.Context, peer yacymodel.Hash) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.active, peer)
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
