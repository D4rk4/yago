package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

const (
	announceHelloPeerCount = 30
	announceMaxGreets      = 16
)

type seedlistFetcher interface {
	Fetch(ctx context.Context, url string) ([]yacymodel.Seed, error)
}

type peerGreeter interface {
	Greet(
		ctx context.Context,
		endpoint string,
		self yacymodel.Seed,
		count int,
	) (greetResult, error)
}

type peerAnnouncement struct {
	settings BootstrapSettings
	fetcher  seedlistFetcher
	greeter  peerGreeter
	status   RuntimeStatus
	registry TrustedSeedSink
}

func newPeerAnnouncement(
	settings BootstrapSettings,
	fetcher seedlistFetcher,
	greeter peerGreeter,
	status RuntimeStatus,
	registry TrustedSeedSink,
) *peerAnnouncement {
	return &peerAnnouncement{
		settings: settings,
		fetcher:  fetcher,
		greeter:  greeter,
		status:   status,
		registry: registry,
	}
}

func (a *peerAnnouncement) Run(ctx context.Context) {
	a.Announce(ctx)

	ticker := time.NewTicker(a.settings.AnnounceInterval)
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

func (a *peerAnnouncement) Announce(ctx context.Context) {
	self := a.status.SelfSeed(ctx)
	targets := a.discover(ctx)

	for i, target := range targets {
		if i >= announceMaxGreets {
			break
		}

		result, err := a.greeter.Greet(ctx, target.endpoint, self, announceHelloPeerCount)
		if err != nil {
			slog.WarnContext(
				ctx,
				"peer greet failed",
				slog.String("peer", target.hash.String()),
				slog.String("endpoint", target.endpoint),
				slog.Any("error", err),
			)

			continue
		}
		if result.YourType == yacymodel.PeerJunior {
			slog.WarnContext(
				ctx,
				"peer reported us as junior",
				slog.String("peer", target.hash.String()),
				slog.String("endpoint", target.endpoint),
				slog.String("reportedAddress", result.YourIP),
			)
		}
		a.registry.Absorb(ctx, result.Known...)
	}
}

func (a *peerAnnouncement) discover(ctx context.Context) []greetTarget {
	var targets []greetTarget

	for _, source := range a.settings.SeedlistURLs {
		seeds, err := a.fetcher.Fetch(ctx, source)
		if err != nil {
			slog.WarnContext(
				ctx,
				"seedlist fetch failed",
				slog.String("url", source),
				slog.Any("error", err),
			)

			continue
		}
		a.registry.Absorb(ctx, seeds...)
		for _, seed := range seeds {
			if endpoint, ok := seed.NetworkAddress(); ok {
				targets = append(targets, greetTarget{hash: seed.Hash, endpoint: endpoint})
			}
		}
	}

	return targets
}
