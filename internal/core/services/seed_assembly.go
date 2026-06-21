package services

import (
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const seedUTCLayout = "2006-01-02T15:04:05"

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
	timestamp := now.UTC().Format(seedUTCLayout)
	seed := yacymodel.Seed{
		Hash:     identity.Hash,
		Name:     yacymodel.Some(identity.Name),
		Port:     yacymodel.Some(yacymodel.Port(identity.Port)),
		Flags:    yacymodel.Some(identity.Flags),
		PeerType: yacymodel.Some(yacymodel.PeerSenior),
		Version:  yacymodel.Some(version),
		Uptime:   yacymodel.Some(uptimeMinutes),
		UTC:      yacymodel.Some(timestamp),
		LastSeen: yacymodel.Some(timestamp),
		RWICount: yacymodel.Some(counts.rwi),
		URLCount: yacymodel.Some(counts.url),
	}
	if host, err := yacymodel.ParseHost(identity.Host); err == nil {
		seed.IP = yacymodel.Some(host)
	}
	return seed
}
