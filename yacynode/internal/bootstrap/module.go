package bootstrap

import (
	"context"
	"net/http"
)

type Module struct {
	announcement *PeerAnnouncement
}

func New(
	client *http.Client,
	networkName string,
	settings BootstrapSettings,
	status RuntimeStatus,
	sink TrustedSeedSink,
) Module {
	return Module{
		announcement: newPeerAnnouncement(
			settings,
			newHTTPSeedlistFetcher(client),
			newHTTPPeerGreeter(client, networkName),
			status,
			sink,
		),
	}
}

func (m Module) Run(ctx context.Context) {
	m.announcement.Run(ctx)
}
