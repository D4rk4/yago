package yagonode

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
	"github.com/D4rk4/yago/yacynode/internal/peernews"
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
	roster   peerroster.Roster
	news     *peernews.Pool
}

type peerExchangeRuntime struct {
	announcer peerannouncement.Announcer
}

var (
	openPeerRoster  = peerroster.Open
	openPeerMailbox = peermessage.OpenMailbox
)

func openObservedPeerRoster(
	vault *vault.Vault,
	peer *metrics.PeerMetrics,
) (peerroster.Roster, error) {
	roster, err := openPeerRoster(vault, time.Now, reservoirCapacity, activeSetCapacity)
	if err != nil {
		return nil, fmt.Errorf("open peer roster: %w", err)
	}
	var observer peerMetricsObserver
	if peer != nil {
		observer = peer
	}

	return observePeerRoster(context.Background(), roster, observer), nil
}

func newNodeStatusReport(
	identity nodeidentity.Identity,
	storage nodeStorage,
	roster peerroster.Roster,
	news *peernews.Pool,
) nodestatus.Report {
	return nodestatus.NewReport(identity, nodestatus.ReportSources{
		RWI:   storage.postings,
		URLs:  storage.urlDirectory,
		Peers: roster,
		News:  news,
	})
}

func openPeerRosterAndNews(
	vault *vault.Vault,
	peer *metrics.PeerMetrics,
) (peerroster.Roster, *peernews.Pool, error) {
	roster, err := openObservedPeerRoster(vault, peer)
	if err != nil {
		return nil, nil, err
	}
	news, err := openRuntimePeerNews(vault, time.Now)
	if err != nil {
		return nil, nil, fmt.Errorf("open peer news: %w", err)
	}

	return roster, news, nil
}

func (p peerExchange) assemble() (peerExchangeRuntime, error) {
	var announceObserver peerannouncement.Observer
	var seedObserver bootstrap.SeedImportObserver
	if p.peer != nil {
		announceObserver = p.peer
		seedObserver = p.peer
	}
	mailbox, err := openPeerMailbox(p.vault, time.Now)
	if err != nil {
		return peerExchangeRuntime{}, fmt.Errorf("open peer message mailbox: %w", err)
	}

	peeradmission.MountHello(
		p.router,
		p.identity,
		peeradmission.HelloExchange{
			Status:       peeringStatus{report: p.report, networkName: p.config.NetworkName},
			Reachability: p.roster,
			Client:       p.client,
			News:         p.news,
		},
	)
	seedlist.Mount(p.router, p.report, p.roster)
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
				News:           p.news,
			},
			p.report,
			bootstrap.NewObserved(p.client, p.config.SeedlistURLs, seedObserver),
			p.roster,
		),
	}, nil
}
