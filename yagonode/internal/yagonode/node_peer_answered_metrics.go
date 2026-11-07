package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

func (r observedPeerRoster) ObserveAnsweredCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
	endpoint peerwire.Endpoint,
) {
	r.Roster.ObserveAnsweredCaller(ctx, caller, classification, endpoint)
	r.observe(ctx)
}

func (r observedPeerRoster) ObserveAnsweredResponder(
	ctx context.Context,
	responder yagomodel.Seed,
	endpoint peerwire.Endpoint,
) {
	r.Roster.ObserveAnsweredResponder(ctx, responder, endpoint)
	r.observe(ctx)
}
