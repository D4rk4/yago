package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/bootstrap"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodeidentity"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodestatus"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/peeradmission"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/peerannouncement"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/peerroster"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

type peerExchange struct {
	router   httpguard.WireRouter
	identity nodeidentity.Identity
	report   nodestatus.Report
	config   nodeConfig
	vault    *vault.Vault
	client   *http.Client
}

func (p peerExchange) assemble() (peerannouncement.Announcer, error) {
	roster, err := peerroster.Open(p.vault, time.Now, reservoirCapacity, activeSetCapacity)
	if err != nil {
		return nil, fmt.Errorf("open peer roster: %w", err)
	}

	peeradmission.MountHello(
		p.router,
		p.identity,
		peeringStatus{report: p.report, networkName: p.config.NetworkName},
		roster,
		p.client,
	)

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
