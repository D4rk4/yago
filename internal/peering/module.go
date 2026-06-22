package peering

import (
	"math/rand/v2"
	"net"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/httpguard"
)

type Module struct {
	HelloEndpoint http.Handler
	Directory     PeerDirectory
	Registry      *TrustedSeedRegistry
}

func New(
	guard httpguard.RequestGuard,
	status RuntimeStatus,
	client *http.Client,
	trustedSeedCapacity int,
	trustedProxies []*net.IPNet,
) Module {
	registry := NewTrustedSeedRegistry(trustedSeedCapacity)
	directory := newPeerDirectory(newCallerBackPing(client), registry, rand.Shuffle, status)

	return Module{
		HelloEndpoint: helloEndpoint{
			guard:          guard,
			status:         status,
			peers:          directory,
			trustedProxies: trustedProxies,
		},
		Directory: directory,
		Registry:  registry,
	}
}
