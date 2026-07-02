// Package peerannouncement greets known peers on an interval: it announces this
// node to them and reports their reachability to the peer roster. It owns no peer
// data — it discovers candidates from the seed source on a cold start, reads
// targets from the roster, and writes reachability observations back.
package peerannouncement

import (
	"context"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/bootstrap"
)

type SelfSeed interface {
	SelfSeed(ctx context.Context) yacymodel.Seed
}

type Announcer interface {
	Run(ctx context.Context)
}

type Observer interface {
	ObservePeerProbeFailure()
}

type PeerNews interface {
	RotateSeedNews(ctx context.Context)
	AcceptNewsAttachment(ctx context.Context, encoded string)
}

type Config struct {
	Client         *http.Client
	NetworkName    string
	Interval       time.Duration
	GreetsPerCycle int
	Observer       Observer
	News           PeerNews
}

func New(
	cfg Config,
	self SelfSeed,
	seeds bootstrap.SeedSource,
	roster peerRoster,
) Announcer {
	return &announcer{
		interval:       cfg.Interval,
		greetsPerCycle: cfg.GreetsPerCycle,
		self:           self,
		seeds:          seeds,
		roster:         roster,
		greeter:        newHTTPPeerGreeter(cfg.Client, cfg.NetworkName),
		observer:       cfg.Observer,
		news:           cfg.News,
	}
}
