// Package peerannouncement greets known peers on an interval: it announces this
// node to them and reports their reachability to the peer roster. It owns no peer
// data — it discovers candidates from the seed source on a cold start, reads
// targets from the roster, and writes reachability observations back.
package peerannouncement

import (
	"context"
	"net/http"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/bootstrap"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/peerroster"
)

type SelfSeed interface {
	SelfSeed(ctx context.Context) yacymodel.Seed
}

type Announcer interface {
	Run(ctx context.Context)
}

type Config struct {
	Client      *http.Client
	NetworkName string
	Interval    time.Duration
}

func New(
	cfg Config,
	self SelfSeed,
	seeds bootstrap.SeedSource,
	roster *peerroster.Roster,
) Announcer {
	return &announcer{
		interval:     cfg.Interval,
		self:         self,
		seeds:        seeds,
		discovery:    roster,
		reachability: roster,
		targets:      roster,
		greeter:      newHTTPPeerGreeter(cfg.Client, cfg.NetworkName),
	}
}
