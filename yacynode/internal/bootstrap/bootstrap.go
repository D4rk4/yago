// Package bootstrap owns outbound peer announcement: it fetches seeds from the
// configured sources, greets peers, and feeds discovered seeds into a
// TrustedSeedSink. RuntimeStatus and TrustedSeedSink are the ports the
// composition root supplies; BootstrapSettings carries the seedlist sources and
// announce interval the composition root has already resolved.
package bootstrap

import (
	"context"
	"net/http"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type RuntimeStatus interface {
	SelfSeed(ctx context.Context) yacymodel.Seed
}

type TrustedSeedSink interface {
	Absorb(ctx context.Context, seeds ...yacymodel.Seed)
}

type BootstrapSettings struct {
	SeedlistURLs     []string
	AnnounceInterval time.Duration
}

type Announcer interface {
	Run(ctx context.Context)
}

func NewAnnouncer(
	client *http.Client,
	networkName string,
	settings BootstrapSettings,
	status RuntimeStatus,
	sink TrustedSeedSink,
) Announcer {
	return newPeerAnnouncement(
		settings,
		newHTTPSeedlistFetcher(client),
		newHTTPPeerGreeter(client, networkName),
		status,
		sink,
	)
}
