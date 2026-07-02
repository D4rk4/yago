// Package peeradmission answers inbound hello requests: it classifies the calling
// peer as senior or junior by probing it back, and returns a random sample of the
// reachable peers it reads from the roster. On a confirmed back-ping it refreshes
// that caller's recency in the roster, but it never introduces a peer learned from
// an inbound request.
package peeradmission

import (
	"context"
	"net/http"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacyproto"
)

type RuntimeStatus interface {
	NetworkName(ctx context.Context) string
	SelfSeed(ctx context.Context) yacymodel.Seed
}

type NewsIntake interface {
	AcceptNewsAttachment(ctx context.Context, encoded string)
}

type HelloExchange struct {
	Status       RuntimeStatus
	Reachability ReachableRoster
	Client       *http.Client
	News         NewsIntake
}

func MountHello(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	exchange HelloExchange,
) {
	httpguard.Mount(
		router,
		yacyproto.PathHello,
		yacyproto.HelloEndpointMethods,
		yacyproto.ParseHelloRequest,
		helloEndpoint{
			identity:     identity,
			status:       exchange.Status,
			probe:        newCallerBackPing(exchange.Client),
			reachability: exchange.Reachability,
			news:         exchange.News,
		}.Serve,
	)
}
