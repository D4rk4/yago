package peerroster

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

const peerResponderObservationFailedMessage = "peer responder observation failed"

func (r *roster) ObserveResponder(ctx context.Context, responder yagomodel.Seed) {
	r.observeResponder(ctx, responder, yagomodel.None[peerwire.Endpoint]())
}

func (r *roster) observeResponder(
	ctx context.Context,
	responder yagomodel.Seed,
	answered yagomodel.Optional[peerwire.Endpoint],
) {
	if r.isSelf(responder.Hash) {
		return
	}
	if _, addressable := responder.NetworkAddress(); !addressable {
		return
	}

	if !r.acquireMembership(ctx) {
		return
	}
	defer r.releaseMembership()

	observed := responder.Copy()
	observation := verifiedRosterEntry(observed, r.now())
	stored, err := r.persistResponderObservation(ctx, observation, answered)
	if err != nil {
		slog.WarnContext(
			ctx,
			peerResponderObservationFailedMessage,
			slog.String("peer", responder.Hash.String()),
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

func (r *roster) persistResponderObservation(
	ctx context.Context,
	observation rosterEntry,
	answered yagomodel.Optional[peerwire.Endpoint],
) (bool, error) {
	if r.isSelf(observation.seed.Hash) {
		return false, nil
	}
	stored, err := r.persistObservation(ctx, observation, false, answered)
	if err != nil {
		return false, fmt.Errorf("observe responder: %w", err)
	}

	return stored, nil
}
