// Package peeradmission answers inbound hello requests: it classifies the calling
// peer as senior or junior by probing it back, and returns a random sample of the
// reachable peers it reads from the roster. On a confirmed back-ping it refreshes
// that caller's recency in the roster, but it never introduces a peer learned from
// an inbound request.
package peeradmission

import (
	"context"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type RuntimeStatus interface {
	NetworkName(ctx context.Context) string
	SelfSeed(ctx context.Context) yacymodel.Seed
}

func MountHello(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	status RuntimeStatus,
	reachability reachableRoster,
	client *http.Client,
) {
	httpguard.Mount(
		router,
		yacyproto.PathHello,
		yacyproto.HelloEndpointMethods,
		yacyproto.ParseHelloRequest,
		helloEndpoint{
			identity:     identity,
			status:       status,
			probe:        newCallerBackPing(client),
			reachability: reachability,
		}.Serve,
	)
}
