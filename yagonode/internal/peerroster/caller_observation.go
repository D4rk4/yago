package peerroster

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const peerCallerObservationFailedMessage = "peer caller observation failed"

func (r *roster) ObserveCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
) {
	if classification != yagomodel.PeerJunior && classification != yagomodel.PeerSenior {
		return
	}
	if _, addressable := caller.NetworkAddress(); !addressable {
		return
	}
	r.membershipMu.Lock()
	defer r.membershipMu.Unlock()

	observed := caller.Copy()
	observed.PeerType = yagomodel.Some(classification)
	if err := r.persistCallerObservation(ctx, observed); err != nil {
		slog.WarnContext(
			ctx,
			peerCallerObservationFailedMessage,
			slog.String("peer", caller.Hash.String()),
			slog.Any("error", err),
		)

		return
	}

	r.mu.Lock()
	if classification == yagomodel.PeerJunior {
		delete(r.active, observed.Hash)
	} else if _, active := r.active[observed.Hash]; active || len(r.active) < r.activeCap {
		r.active[observed.Hash] = observed
	}
	r.mu.Unlock()

	r.evictOverflow(ctx)
	r.invalidateCandidateSnapshot()
}

func (r *roster) persistCallerObservation(
	ctx context.Context,
	observed yagomodel.Seed,
) error {
	if err := r.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := r.peers.Put(tx, r.key(observed.Hash), rosterEntry{
			seed: observed, lastSeen: r.now(),
		}); err != nil {
			return fmt.Errorf("store caller: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("observe caller: %w", err)
	}

	return nil
}

func locallyJunior(seed yagomodel.Seed) bool {
	classification, known := seed.PeerType.Get()

	return known && classification == yagomodel.PeerJunior
}
