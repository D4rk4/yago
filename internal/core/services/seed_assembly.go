package services

import (
	"strconv"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const (
	seedRWICount  = "ICount"
	seedURLCount  = "LCount"
	seedUTCLayout = "2006-01-02T15:04:05"
)

type seedCounts struct {
	rwi int
	url int
}

func (i Identity) assembleSeed(
	now time.Time,
	uptimeMinutes int,
	version string,
	counts seedCounts,
) yacymodel.Seed {
	return yacymodel.Seed{
		yacymodel.SeedHash:     string(i.hash),
		yacymodel.SeedName:     i.name,
		yacymodel.SeedIP:       i.host,
		yacymodel.SeedPort:     strconv.Itoa(i.port),
		yacymodel.SeedFlags:    i.flags.String(),
		yacymodel.SeedPeerType: string(yacymodel.PeerSenior),
		yacymodel.SeedVersion:  version,
		yacymodel.SeedUptime:   strconv.Itoa(uptimeMinutes),
		yacymodel.SeedUTC:      now.UTC().Format(seedUTCLayout),
		yacymodel.SeedLastSeen: now.UTC().Format(seedUTCLayout),
		seedRWICount:           strconv.Itoa(counts.rwi),
		seedURLCount:           strconv.Itoa(counts.url),
	}
}
