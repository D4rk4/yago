package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yacynode/internal/bootstrap"
	"github.com/D4rk4/yago/yacynode/internal/hostlinks"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
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
}

var (
	openPeerRoster  = peerroster.Open
	openPeerMailbox = peermessage.OpenMailbox
)

func (p peerExchange) assemble() (peerannouncement.Announcer, error) {
	roster, err := openPeerRoster(p.vault, time.Now, reservoirCapacity, activeSetCapacity)
	if err != nil {
		return nil, fmt.Errorf("open peer roster: %w", err)
	}
	mailbox, err := openPeerMailbox(p.vault, time.Now)
	if err != nil {
		return nil, fmt.Errorf("open peer message mailbox: %w", err)
	}

	peeradmission.MountHello(
		p.router,
		p.identity,
		peeringStatus{report: p.report, networkName: p.config.NetworkName},
		roster,
		p.client,
	)
	seedlist.Mount(p.router, p.report, roster)
	hostlinks.Mount(p.router, p.config.NetworkName, p.report, hostlinks.NoIncomingHostLinks{})
	peermessage.Mount(p.router, p.identity, mailbox)
	peerprofile.Mount(p.router, p.identity, peerprofile.NoPeerProfile{})
	sharedblacklist.Mount(p.router, sharedblacklist.NoSharedBlacklists{})

	return peerannouncement.New(
		peerannouncement.Config{
			Client:         p.client,
			NetworkName:    p.config.NetworkName,
			Interval:       p.config.AnnounceInterval,
			GreetsPerCycle: p.config.GreetsPerCycle,
		},
		p.report,
		bootstrap.New(p.client, p.config.SeedlistURLs),
		roster,
	), nil
}
