package services

import (
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type seedCounts struct {
	rwi int
	url int
}

func assembleSeed(
	identity yacymodel.PeerIdentity,
	now time.Time,
	uptimeMinutes int,
	version string,
	counts seedCounts,
) yacymodel.Seed {
	seed := yacymodel.Seed{
		Hash:     identity.Hash,
		Name:     yacymodel.Some(identity.Name),
		Port:     yacymodel.Some(yacymodel.Port(identity.Port)),
		Flags:    yacymodel.Some(identity.Flags),
		PeerType: yacymodel.Some(yacymodel.PeerSenior),
		Version:  yacymodel.Some(yacymodel.YaCyVersion(version)),
		Uptime:   yacymodel.Some(uptimeMinutes),
		UTC:      yacymodel.Some(yacymodel.SeedUTCOffsetFromTime(now)),
		LastSeen: yacymodel.Some(yacymodel.NewSeedLastSeenUTC(now)),
		RWICount: yacymodel.Some(counts.rwi),
		URLCount: yacymodel.Some(counts.url),
	}
	if host, err := yacymodel.ParseHost(identity.Host); err == nil {
		seed.IP = yacymodel.Some(host)
	}
	return seed
}
