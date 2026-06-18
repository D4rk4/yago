package services

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const (
	announceHelloPeerCount = 30
	announceMaxGreets      = 16
)

type bootstrapConfig interface {
	SeedlistURLs() []string
	BootstrapPeers() []string
	AnnounceInterval() time.Duration
}

type seedlistFetcher interface {
	Fetch(ctx context.Context, url string) ([]yacymodel.Seed, error)
}

type peerGreeter interface {
	Greet(
		ctx context.Context,
		endpoint string,
		self yacymodel.Seed,
		count int,
	) (ports.GreetResult, error)
}

type trustedSeedSink interface {
	Absorb(ctx context.Context, seeds ...yacymodel.Seed)
}

type PeerAnnouncement struct {
	config   bootstrapConfig
	fetcher  seedlistFetcher
	greeter  peerGreeter
	status   contracts.RuntimeStatus
	registry trustedSeedSink
}

func NewPeerAnnouncement(
	config bootstrapConfig,
	fetcher seedlistFetcher,
	greeter peerGreeter,
	status contracts.RuntimeStatus,
	registry trustedSeedSink,
) *PeerAnnouncement {
	return &PeerAnnouncement{
		config:   config,
		fetcher:  fetcher,
		greeter:  greeter,
		status:   status,
		registry: registry,
	}
}

func (a *PeerAnnouncement) Run(ctx context.Context) {
	a.Announce(ctx)

	ticker := time.NewTicker(a.config.AnnounceInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.Announce(ctx)
		}
	}
}

func (a *PeerAnnouncement) Announce(ctx context.Context) {
	self := a.status.Snapshot(ctx).Seed
	endpoints := a.discover(ctx)

	for i, endpoint := range endpoints {
		if i >= announceMaxGreets {
			break
		}

		result, err := a.greeter.Greet(ctx, endpoint, self, announceHelloPeerCount)
		if err != nil {
			slog.DebugContext(ctx, "peer greet failed", "endpoint", endpoint, "error", err)

			continue
		}
		a.registry.Absorb(ctx, result.Known...)
	}
}

func (a *PeerAnnouncement) discover(ctx context.Context) []string {
	endpoints := append([]string(nil), a.config.BootstrapPeers()...)

	for _, source := range a.config.SeedlistURLs() {
		seeds, err := a.fetcher.Fetch(ctx, source)
		if err != nil {
			slog.WarnContext(ctx, "seedlist fetch failed", "url", source, "error", err)

			continue
		}
		a.registry.Absorb(ctx, seeds...)
		for _, seed := range seeds {
			if endpoint, ok := seedEndpoint(seed); ok {
				endpoints = append(endpoints, endpoint)
			}
		}
	}

	return endpoints
}

func seedEndpoint(seed yacymodel.Seed) (string, bool) {
	host := seed[yacymodel.SeedIP]
	if host == "" {
		return "", false
	}
	port, err := seed.Port()
	if err != nil || port <= 0 {
		return "", false
	}

	return net.JoinHostPort(host, strconv.Itoa(port)), true
}
