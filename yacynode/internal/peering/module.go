package peering

import (
	"math/rand/v2"
	"net"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
)

type Module struct {
	HelloEndpoint http.Handler
	Directory     PeerDirectory
	Registry      *TrustedSeedRegistry
}

type Config struct {
	TrustedSeedCapacity int
	TrustedProxies      []*net.IPNet
}

func New(
	guard httpguard.RequestGuard,
	respond httpguard.WireResponder,
	status RuntimeStatus,
	client *http.Client,
	cfg Config,
) Module {
	registry := NewTrustedSeedRegistry(cfg.TrustedSeedCapacity)
	directory := newPeerDirectory(newCallerBackPing(client), registry, rand.Shuffle, status)

	return Module{
		HelloEndpoint: helloEndpoint{
			guard:          guard,
			respond:        respond,
			status:         status,
			peers:          directory,
			trustedProxies: cfg.TrustedProxies,
		},
		Directory: directory,
		Registry:  registry,
	}
}
