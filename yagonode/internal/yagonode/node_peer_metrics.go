package yagonode

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

var errPeerObservationsUnavailable = errors.New("peer observations unavailable")

type peerMetricsObserver interface {
	ObservePeerRoster(known, active int)
}

type observedPeerRoster struct {
	peerroster.Roster
	observer peerMetricsObserver
}

type observedKnownPeerCounter interface {
	ObservedKnownPeerCount(ctx context.Context) (int, error)
}

func observePeerRoster(
	ctx context.Context,
	roster peerroster.Roster,
	observer peerMetricsObserver,
) peerroster.Roster {
	if observer == nil {
		return roster
	}

	observed := observedPeerRoster{Roster: roster, observer: observer}
	observed.observe(ctx)

	return observed
}

func (r observedPeerRoster) Discover(ctx context.Context, seeds ...yagomodel.Seed) {
	r.Roster.Discover(ctx, seeds...)
	r.observe(ctx)
}

func (r observedPeerRoster) ConfirmReachable(ctx context.Context, peer yagomodel.Hash) {
	r.Roster.ConfirmReachable(ctx, peer)
	r.observe(ctx)
}

func (r observedPeerRoster) ConfirmUnreachable(ctx context.Context, peer yagomodel.Hash) {
	r.Roster.ConfirmUnreachable(ctx, peer)
	r.observe(ctx)
}

func (r observedPeerRoster) PeerObservations(
	ctx context.Context,
) ([]peerroster.PeerObservation, int, int, error) {
	reader, ok := r.Roster.(peerroster.ObservationReader)
	if !ok {
		return nil, 0, 0, errPeerObservationsUnavailable
	}

	observations, known, reachable, err := reader.PeerObservations(ctx)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read peer observations: %w", err)
	}

	return observations, known, reachable, nil
}

func (r observedPeerRoster) PeerObservation(
	ctx context.Context,
	peer yagomodel.Hash,
) (peerroster.PeerObservation, bool, error) {
	reader, ok := r.Roster.(peerroster.ObservationReader)
	if !ok {
		return peerroster.PeerObservation{}, false, errPeerObservationsUnavailable
	}

	observation, found, err := reader.PeerObservation(ctx, peer)
	if err != nil {
		return peerroster.PeerObservation{}, false, fmt.Errorf(
			"read peer observation: %w",
			err,
		)
	}

	return observation, found, nil
}

func (r observedPeerRoster) ObservedKnownPeerCount(ctx context.Context) (int, error) {
	observed, ok := r.Roster.(observedKnownPeerCounter)
	if !ok {
		return r.KnownPeerCount(ctx), nil
	}
	count, err := observed.ObservedKnownPeerCount(ctx)
	if err != nil {
		return 0, fmt.Errorf("count observed peers: %w", err)
	}

	return count, nil
}

func (r observedPeerRoster) observe(ctx context.Context) {
	r.observer.ObservePeerRoster(
		r.KnownPeerCount(ctx),
		r.ReachablePeerCount(ctx),
	)
}
