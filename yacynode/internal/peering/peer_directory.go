package peering

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type trustedSeedSource interface {
	Trusted(ctx context.Context) []yacymodel.Seed
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
	probe   callerReachabilityProbe
	trusted trustedSeedSource
	shuffle seedShuffler
	status  RuntimeStatus
}

func newPeerDirectory(
	probe callerReachabilityProbe,
	trusted trustedSeedSource,
	shuffle seedShuffler,
	status RuntimeStatus,
) peerDirectory {
	return peerDirectory{probe: probe, trusted: trusted, shuffle: shuffle, status: status}
}

func (d peerDirectory) Hello(
	ctx context.Context,
	caller yacymodel.Seed,
	count int,
) (HelloOutcome, error) {
	return HelloOutcome{
		CallerType: d.classifyCaller(ctx, caller),
		Known:      d.sampleSeeds(d.trusted.Trusted(ctx), count),
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
