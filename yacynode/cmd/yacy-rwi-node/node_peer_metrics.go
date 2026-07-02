package main

import (
	"context"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/peerroster"
)

type peerMetricsObserver interface {
	ObservePeerRoster(known, active int)
}

type observedPeerRoster struct {
	peerroster.Roster
	observer peerMetricsObserver
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

func (r observedPeerRoster) Discover(ctx context.Context, seeds ...yacymodel.Seed) {
	r.Roster.Discover(ctx, seeds...)
	r.observe(ctx)
}

func (r observedPeerRoster) ConfirmReachable(ctx context.Context, peer yacymodel.Hash) {
	r.Roster.ConfirmReachable(ctx, peer)
	r.observe(ctx)
}

func (r observedPeerRoster) ConfirmUnreachable(ctx context.Context, peer yacymodel.Hash) {
	r.Roster.ConfirmUnreachable(ctx, peer)
	r.observe(ctx)
}

func (r observedPeerRoster) observe(ctx context.Context) {
	r.observer.ObservePeerRoster(
		r.KnownPeerCount(ctx),
		r.ReachablePeerCount(ctx),
	)
}
