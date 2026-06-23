// Package peering owns the hello endpoint and the trusted-seed store that seeds
// outbound greetings. Its published ports, RuntimeStatus and TrustedSeeds,
// describe what the endpoint needs and the shared seed store the composition root
// hands to both the endpoint and the bootstrap announcer.
package peering

import (
	"context"
	"math/rand/v2"
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

type TrustedSeeds interface {
	Absorb(ctx context.Context, seeds ...yacymodel.Seed)
	Trusted(ctx context.Context) []yacymodel.Seed
}

func NewTrustedSeeds(capacity int) TrustedSeeds {
	return newTrustedSeedRegistry(capacity)
}

func MountHello(
	router httpguard.WireRouter,
	identity nodeidentity.Identity,
	status RuntimeStatus,
	seeds TrustedSeeds,
	client *http.Client,
) {
	directory := newPeerDirectory(newCallerBackPing(client), seeds, rand.Shuffle, status)
	httpguard.Mount(
		router,
		yacyproto.PathHello,
		yacyproto.HelloEndpointMethods,
		yacyproto.ParseHelloRequest,
		helloEndpoint{identity: identity, status: status, peers: directory}.Serve,
	)
}
