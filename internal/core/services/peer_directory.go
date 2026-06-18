package services

import (
	"context"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type trustedSeedSource interface {
	Trusted(ctx context.Context) []yacymodel.Seed
}

type PeerDirectory struct {
	pinger  ports.PeerPinger
	trusted trustedSeedSource
	shuffle seedShuffler
}

func NewPeerDirectory(
	pinger ports.PeerPinger,
	trusted trustedSeedSource,
	shuffle seedShuffler,
) *PeerDirectory {
	return &PeerDirectory{
		pinger:  pinger,
		trusted: trusted,
		shuffle: shuffle,
	}
}

func (d *PeerDirectory) Hello(
	ctx context.Context,
	caller yacymodel.Seed,
	count int,
) (contracts.HelloOutcome, error) {
	return contracts.HelloOutcome{
		CallerType: d.classifyCaller(ctx, caller),
		Known:      selectAnnouncedSeeds(d.trusted.Trusted(ctx), count, d.shuffle),
	}, nil
}

func (d *PeerDirectory) classifyCaller(
	ctx context.Context,
	caller yacymodel.Seed,
) yacymodel.PeerType {
	if !advertisesReachableEndpoint(caller) {
		return yacymodel.PeerJunior
	}
	if err := d.pinger.Ping(ctx, caller); err != nil {
		return yacymodel.PeerJunior
	}

	return yacymodel.PeerSenior
}

func advertisesReachableEndpoint(caller yacymodel.Seed) bool {
	if caller[yacymodel.SeedIP] == "" {
		return false
	}
	port, err := caller.Port()

	return err == nil && port > 0
}
