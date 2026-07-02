package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/bootstrap"
	"github.com/D4rk4/yago/yacynode/internal/hostlinks"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/metrics"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/nodestatus"
	"github.com/D4rk4/yago/yacynode/internal/peeradmission"
	"github.com/D4rk4/yago/yacynode/internal/peerannouncement"
	"github.com/D4rk4/yago/yacynode/internal/peermessage"
	"github.com/D4rk4/yago/yacynode/internal/peerprofile"
	"github.com/D4rk4/yago/yacynode/internal/peerroster"
	"github.com/D4rk4/yago/yacynode/internal/seedlist"
	"github.com/D4rk4/yago/yacynode/internal/sharedblacklist"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type peerExchange struct {
	router   httpguard.WireRouter
	identity nodeidentity.Identity
	report   nodestatus.Report
	config   nodeConfig
	vault    *vault.Vault
	client   *http.Client
	peer     *metrics.PeerMetrics
	host     hostlinks.IncomingHostLinks
}

type peerExchangeRuntime struct {
	announcer peerannouncement.Announcer
	roster    peerroster.Roster
}

var (
	openPeerRoster  = peerroster.Open
	openPeerMailbox = peermessage.OpenMailbox
)

func (p peerExchange) assemble() (peerExchangeRuntime, error) {
	roster, err := openPeerRoster(p.vault, time.Now, reservoirCapacity, activeSetCapacity)
	if err != nil {
		return peerExchangeRuntime{}, fmt.Errorf("open peer roster: %w", err)
	}
	var rosterObserver peerMetricsObserver
	var announceObserver peerannouncement.Observer
	var seedObserver bootstrap.SeedImportObserver
	if p.peer != nil {
		rosterObserver = p.peer
		announceObserver = p.peer
		seedObserver = p.peer
	}
	roster = observePeerRoster(context.Background(), roster, rosterObserver)
	mailbox, err := openPeerMailbox(p.vault, time.Now)
	if err != nil {
		return peerExchangeRuntime{}, fmt.Errorf("open peer message mailbox: %w", err)
	}

	peeradmission.MountHello(
		p.router,
		p.identity,
		peeringStatus{report: p.report, networkName: p.config.NetworkName},
		roster,
		p.client,
	)
	seedlist.Mount(p.router, p.report, roster)
	hostlinks.Mount(p.router, p.config.NetworkName, p.report, p.host)
	peermessage.Mount(p.router, p.identity, mailbox)
	peerprofile.Mount(p.router, p.identity, peerprofile.NewProfileFile(p.config.DataDir))
	sharedblacklist.Mount(
		p.router,
		p.config.NetworkName,
		sharedblacklist.NewFileBlacklists(p.config.DataDir),
	)

	return peerExchangeRuntime{
		announcer: peerannouncement.New(
			peerannouncement.Config{
				Client:         p.client,
				NetworkName:    p.config.NetworkName,
				Interval:       p.config.AnnounceInterval,
				GreetsPerCycle: p.config.GreetsPerCycle,
				Observer:       announceObserver,
			},
			p.report,
			bootstrap.NewObserved(p.client, p.config.SeedlistURLs, seedObserver),
			roster,
		),
		roster: roster,
	}, nil
}
