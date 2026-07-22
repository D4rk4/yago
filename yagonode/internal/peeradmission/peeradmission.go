// Package peeradmission answers inbound hello requests: it classifies the calling
// peer by probing it back, and returns a bounded freshest set of reachable peers.
package peeradmission

import (
	"context"
	"net/http"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

type RuntimeStatus interface {
	NetworkName(ctx context.Context) string
	SelfSeed(ctx context.Context) yagomodel.Seed
}

type NewsIntake interface {
	AcceptNewsAttachment(ctx context.Context, encoded string)
}

type HelloExchange struct {
	Status        RuntimeStatus
	Reachability  ReachableRoster
	Client        *http.Client
	News          NewsIntake
	PreferHTTPS   bool
	NetworkAccess yagoproto.NetworkAccess
}

func MountHello(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	exchange HelloExchange,
) {
	endpoint := helloEndpoint{
		identity: identity,
		status:   exchange.Status,
		probe: newCallerBackPing(
			exchange.Client, exchange.PreferHTTPS, exchange.NetworkAccess,
		),
		reachability: exchange.Reachability,
		news:         exchange.News,
	}
	httpguard.Mount(
		router,
		yagoproto.PathHello,
		yagoproto.HelloEndpointMethods,
		parseHelloRequestEnvelope,
		endpoint.ServeEnvelope,
	)
}
