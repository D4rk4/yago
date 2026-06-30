package peeradmission

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/peerroster"
)

type helloOutcome struct {
	CallerType yacymodel.PeerType
	Known      []yacymodel.Seed
}

type seedShuffler func(n int, swap func(i, j int))

type callerReachabilityProbe interface {
	Reachable(
		ctx context.Context,
		caller yacymodel.Seed,
		self yacymodel.Hash,
		networkName string,
	) bool
}

type peerDirectory struct {
	probe     callerReachabilityProbe
	reachable peerroster.ReachablePeerSource
	refresher peerroster.ReachabilityRefresher
	shuffle   seedShuffler
	status    RuntimeStatus
}

func newPeerDirectory(
	probe callerReachabilityProbe,
	reachable peerroster.ReachablePeerSource,
	refresher peerroster.ReachabilityRefresher,
	shuffle seedShuffler,
	status RuntimeStatus,
) peerDirectory {
	return peerDirectory{
		probe:     probe,
		reachable: reachable,
		refresher: refresher,
		shuffle:   shuffle,
		status:    status,
	}
}

func (d peerDirectory) Hello(
	ctx context.Context,
	caller yacymodel.Seed,
	count int,
) (helloOutcome, error) {
	return helloOutcome{
		CallerType: d.classifyCaller(ctx, caller),
		Known:      d.sampleSeeds(d.reachable.ReachablePeers(ctx), count),
	}, nil
}

func (d peerDirectory) classifyCaller(
	ctx context.Context,
	caller yacymodel.Seed,
) yacymodel.PeerType {
	if _, ok := caller.NetworkAddress(); !ok {
		return yacymodel.PeerJunior
	}

	if !d.probe.Reachable(ctx, caller, d.status.SelfSeed(ctx).Hash, d.status.NetworkName(ctx)) {
		return yacymodel.PeerJunior
	}

	d.refresher.Reachable(ctx, caller.Hash)

	return yacymodel.PeerSenior
}

func (d peerDirectory) sampleSeeds(seeds []yacymodel.Seed, count int) []yacymodel.Seed {
	picked := make([]yacymodel.Seed, len(seeds))
	copy(picked, seeds)

	d.shuffle(len(picked), func(i, j int) {
		picked[i], picked[j] = picked[j], picked[i]
	})

	if count > 0 && count < len(picked) {
		picked = picked[:count]
	}

	return picked
}
