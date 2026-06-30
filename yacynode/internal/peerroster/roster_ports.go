package peerroster

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type PeerDiscovery interface {
	Discover(ctx context.Context, seeds ...yacymodel.Seed)
}

type PeerReachability interface {
	Reachable(ctx context.Context, peer yacymodel.Hash)
	Unreachable(ctx context.Context, peer yacymodel.Hash)
}

type ReachabilityRefresher interface {
	Reachable(ctx context.Context, peer yacymodel.Hash)
}

type ReachablePeerSource interface {
	ReachablePeers(ctx context.Context) []yacymodel.Seed
}

type GreetTargetSource interface {
	GreetTargets(ctx context.Context) []yacymodel.Seed
}

var (
	_ PeerDiscovery         = (*Roster)(nil)
	_ PeerReachability      = (*Roster)(nil)
	_ ReachabilityRefresher = (*Roster)(nil)
	_ ReachablePeerSource   = (*Roster)(nil)
	_ GreetTargetSource     = (*Roster)(nil)
)
