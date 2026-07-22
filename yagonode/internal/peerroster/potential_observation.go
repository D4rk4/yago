package peerroster

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
)

const peerPotentialObservationFailedMessage = "peer potential observation failed"

func (r *roster) ObservePotential(ctx context.Context, potential yagomodel.Seed) {
	if r.isSelf(potential.Hash) {
		return
	}
	if _, addressable := potential.NetworkAddress(); !addressable {
		return
	}
	potential = potential.Copy()
	potential.PeerType = yagomodel.Some(yagomodel.PeerVirgin)

	if !r.acquireMembership(ctx) {
		return
	}
	defer r.releaseMembership()

	now := r.now()
	inserted, err := r.persistPotentialObservation(ctx, rosterEntry{
		seed:      potential,
		lastSeen:  now,
		expiresAt: now.Add(peerPassiveRetention),
	})
	if err != nil {
		slog.WarnContext(
			ctx,
			peerPotentialObservationFailedMessage,
			slog.String("peer", potential.Hash.String()),
			slog.Any("error", err),
		)

		return
	}
	if !inserted {
		return
	}
	r.evictOverflow(ctx)
	r.invalidateCandidateSnapshot()
}

func (r *roster) persistPotentialObservation(
	ctx context.Context,
	observation rosterEntry,
) (bool, error) {
	inserted, err := r.persistObservation(ctx, observation, true)
	if err != nil {
		return false, fmt.Errorf("observe potential peer: %w", err)
	}

	return inserted, nil
}
