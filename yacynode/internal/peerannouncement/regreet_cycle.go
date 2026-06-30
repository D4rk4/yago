package peerannouncement

import (
	"context"
	"log/slog"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/bootstrap"
)

const (
	announceHelloPeerCount = 30
	announceMaxGreets      = 16
)

type peerGreeter interface {
	Greet(
		ctx context.Context,
		endpoint string,
		self yacymodel.Seed,
		count int,
	) (greetResult, error)
}

type peerRoster interface {
	Discover(ctx context.Context, seeds ...yacymodel.Seed)
	ConfirmReachable(ctx context.Context, peer yacymodel.Hash)
	ConfirmUnreachable(ctx context.Context, peer yacymodel.Hash)
	FreshestPeers(ctx context.Context, limit int) []yacymodel.Seed
}

type announcer struct {
	interval time.Duration
	self     SelfSeed
	seeds    bootstrap.SeedSource
	roster   peerRoster
	greeter  peerGreeter
}

func (a *announcer) Run(ctx context.Context) {
	a.Announce(ctx)

	ticker := time.NewTicker(a.interval)
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

func (a *announcer) Announce(ctx context.Context) {
	self := a.self.SelfSeed(ctx)
	targets := a.roster.FreshestPeers(ctx, announceMaxGreets)
	if len(targets) == 0 {
		a.roster.Discover(ctx, a.seeds.Fetch(ctx)...)
		targets = a.roster.FreshestPeers(ctx, announceMaxGreets)
	}

	for _, target := range targets {
		endpoint, ok := target.NetworkAddress()
		if !ok {
			continue
		}

		result, err := a.greeter.Greet(ctx, endpoint, self, announceHelloPeerCount)
		if err != nil {
			a.roster.ConfirmUnreachable(ctx, target.Hash)
			slog.WarnContext(
				ctx,
				"peer greet failed",
				slog.String("peer", target.Hash.String()),
				slog.String("endpoint", endpoint),
				slog.Any("error", err),
			)

			continue
		}
		if result.YourType == yacymodel.PeerJunior {
			slog.WarnContext(
				ctx,
				"peer reported us as junior",
				slog.String("peer", target.Hash.String()),
				slog.String("endpoint", endpoint),
				slog.String("reportedAddress", result.YourIP),
			)
		}

		a.roster.ConfirmReachable(ctx, target.Hash)
		a.roster.Discover(ctx, result.Known...)
	}
}
