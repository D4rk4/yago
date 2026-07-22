// Package peerannouncement greets known peers on an interval: it announces this
// node to them and reports their reachability to the peer roster. It owns no peer
// data — it discovers candidates from the seed source on a cold start, reads
// targets from the roster, and writes reachability observations back.
package peerannouncement

import (
	"context"
	"net/http"
	"net/netip"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/bootstrap"
	"github.com/D4rk4/yago/yagoproto"
)

type SelfSeed interface {
	SelfSeed(ctx context.Context) yagomodel.Seed
}

type Announcer interface {
	Run(ctx context.Context)
	// GreetDiscovered runs one verified hello exchange against a peer found
	// outside the roster (LAN discovery); a successful exchange lands the
	// peer and its known seeds in the roster like any greeted peer.
	GreetDiscovered(ctx context.Context, target yagomodel.Seed)
}

type Observer interface {
	ObservePeerProbeFailure()
}

type PeerNews interface {
	RotateSeedNews(ctx context.Context)
	AcceptNewsAttachment(ctx context.Context, encoded string)
}

type Config struct {
	Client                       *http.Client
	NetworkName                  string
	Interval                     time.Duration
	GreetsPerCycle               int
	Observer                     Observer
	News                         PeerNews
	PreferHTTPS                  bool
	NetworkAccess                yagoproto.NetworkAccess
	ExternalReachabilityEvidence *ExternalReachabilityEvidence
	AdmitExternalObserverAddress func(netip.Addr) error
}

func New(
	cfg Config,
	self SelfSeed,
	seeds bootstrap.SeedSource,
	roster peerRoster,
) Announcer {
	return &announcer{
		interval:                cfg.Interval,
		externalRefreshInterval: externalReachabilityRefreshInterval,
		greetsPerCycle:          cfg.GreetsPerCycle,
		self:                    self,
		seeds:                   seeds,
		roster:                  roster,
		greeter: newHTTPPeerGreeter(
			cfg.Client, cfg.NetworkName, cfg.PreferHTTPS, cfg.NetworkAccess,
		),
		observer:                     cfg.Observer,
		news:                         cfg.News,
		externalReachabilityEvidence: cfg.ExternalReachabilityEvidence,
		admitExternalObserverAddress: cfg.AdmitExternalObserverAddress,
		bootstrap: bootstrapRefresh{
			now:      time.Now,
			cooldown: bootstrapRetryCooldown,
		},
	}
}
