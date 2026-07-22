package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
)

type helloPeerRoster struct {
	roster peerroster.Roster
}

func (r helloPeerRoster) FreshestPeers(ctx context.Context, limit int) []yagomodel.Seed {
	if limit <= 0 {
		return nil
	}
	selected := r.roster.ReachablePeers(ctx)
	if len(selected) > limit {
		selected = selected[:limit]
	}

	return selected
}

func (r helloPeerRoster) ObserveCaller(
	ctx context.Context,
	caller yagomodel.Seed,
	classification yagomodel.PeerType,
) {
	r.roster.ObserveCaller(ctx, caller, classification)
}
