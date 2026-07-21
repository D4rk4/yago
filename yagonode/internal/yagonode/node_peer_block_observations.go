package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

func (r blockingRoster) PeerObservations(
	ctx context.Context,
) ([]peerroster.PeerObservation, int, int, error) {
	reader, ok := r.Roster.(peerroster.ObservationReader)
	if !ok {
		return nil, 0, 0, errPeerObservationsUnavailable
	}

	observations, known, _, err := reader.PeerObservations(ctx)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read peer observations: %w", err)
	}

	return observations, known, r.ReachablePeerCount(ctx), nil
}

func (r blockingRoster) PeerObservation(
	ctx context.Context,
	peer yagomodel.Hash,
) (peerroster.PeerObservation, bool, error) {
	reader, ok := r.Roster.(peerroster.ObservationReader)
	if !ok {
		return peerroster.PeerObservation{}, false, errPeerObservationsUnavailable
	}

	observation, found, err := reader.PeerObservation(ctx, peer)
	if err != nil {
		return peerroster.PeerObservation{}, false, fmt.Errorf("read peer observation: %w", err)
	}

	return observation, found, nil
}
