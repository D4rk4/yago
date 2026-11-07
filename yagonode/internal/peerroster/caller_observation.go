package peerroster

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

const peerCallerObservationFailedMessage = "peer caller observation failed"

func (r *roster) ObserveCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
) {
	r.observeCaller(ctx, caller, classification, yagomodel.None[peerwire.Endpoint]())
}

func (r *roster) observeCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
	answered yagomodel.Optional[peerwire.Endpoint],
) {
	if r.isSelf(caller.Hash) {
		return
	}
	if classification != yagomodel.PeerJunior &&
		classification != yagomodel.PeerSenior &&
		classification != yagomodel.PeerPrincipal {
		return
	}
	if _, addressable := caller.NetworkAddress(); !addressable {
		return
	}
	if !r.acquireMembership(ctx) {
		return
	}
	defer r.releaseMembership()

	observed := caller.Copy()
	observed.PeerType = yagomodel.Some(classification)
	now := r.now()
	observation := verifiedRosterEntry(observed, now)
	if classification == yagomodel.PeerJunior {
		observation.verified = false
	}
	stored, err := r.persistCallerObservation(ctx, observation, answered)
	if err != nil {
		slog.WarnContext(
			ctx,
			peerCallerObservationFailedMessage,
			slog.String("peer", caller.Hash.String()),
			slog.Any("error", err),
		)

		return
	}
	if !stored {
		return
	}

	r.replaceActiveMembership(observation, nil)

	r.evictOverflow(ctx)
	r.invalidateCandidateSnapshot()
}

func (r *roster) persistCallerObservation(
	ctx context.Context,
	observation rosterEntry,
	answered yagomodel.Optional[peerwire.Endpoint],
) (bool, error) {
	if r.isSelf(observation.seed.Hash) {
		return false, nil
	}
	stored, err := r.persistObservation(ctx, observation, !observation.verified, answered)
	if err != nil {
		return false, fmt.Errorf("observe caller: %w", err)
	}

	return stored, nil
}
