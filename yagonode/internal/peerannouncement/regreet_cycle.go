package peerannouncement

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/bootstrap"
)

const announceHelloPeerCount = 30

type peerGreeter interface {
	Greet(
		ctx context.Context,
		target yagomodel.Seed,
		self yagomodel.Seed,
		count int,
	) (greetResult, error)
}

type peerRoster interface {
	Discover(ctx context.Context, seeds ...yagomodel.Seed)
	ConfirmReachable(ctx context.Context, peer yagomodel.Hash)
	ConfirmUnreachable(ctx context.Context, peer yagomodel.Hash)
	FreshestPeers(ctx context.Context, limit int) []yagomodel.Seed
}

type announcer struct {
	interval       time.Duration
	greetsPerCycle int
	self           SelfSeed
	seeds          bootstrap.SeedSource
	roster         peerRoster
	greeter        peerGreeter
	observer       Observer
	news           PeerNews
}

func (a *announcer) Run(ctx context.Context) {
	a.roster.Discover(ctx, a.seeds.Fetch(ctx)...)
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
	if a.news != nil {
		a.news.RotateSeedNews(ctx)
	}
	self := a.self.SelfSeed(ctx)
	targets := a.roster.FreshestPeers(ctx, a.greetsPerCycle)

	for _, target := range targets {
		if target.Hash == self.Hash {
			slog.DebugContext(
				ctx,
				"skipped self in greet targets",
				slog.String("peer", target.Hash.String()),
			)

			continue
		}

		endpoint, ok := target.NetworkAddress()
		if !ok {
			continue
		}

		result, err := a.greeter.Greet(ctx, target, self, announceHelloPeerCount)
		if err != nil {
			if a.observer != nil {
				a.observer.ObservePeerProbeFailure()
			}
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
		if result.YourType == yagomodel.PeerJunior {
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
		a.acceptGreetedNews(ctx, result.Known)
	}
}

func (a *announcer) acceptGreetedNews(ctx context.Context, seeds []yagomodel.Seed) {
	if a.news == nil {
		return
	}
	for _, seed := range seeds {
		if attachment := seed.Properties()[yagomodel.SeedNews]; attachment != "" {
			a.news.AcceptNewsAttachment(ctx, attachment)
		}
	}
}
