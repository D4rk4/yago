package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerwire"
)

func (r helloPeerRoster) AnsweredEndpoint(
	ctx context.Context,
	peer yagomodel.Hash,
) (peerwire.Endpoint, bool, error) {
	endpoint, found, err := r.roster.AnsweredEndpoint(ctx, peer)
	if err != nil {
		return peerwire.Endpoint{}, false, fmt.Errorf("read hello peer answered endpoint: %w", err)
	}

	return endpoint, found, nil
}

func (r helloPeerRoster) ObserveAnsweredCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
	endpoint peerwire.Endpoint,
) {
	r.roster.ObserveAnsweredCaller(ctx, caller, classification, endpoint)
}
