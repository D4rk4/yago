package yagonode

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/D4rk4/yago/yagoegress"
	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/bootstrap"
	"github.com/D4rk4/yago/yagonode/internal/hostlinks"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/peeradmission"
	"github.com/D4rk4/yago/yagonode/internal/peerannouncement"
	"github.com/D4rk4/yago/yagonode/internal/peerblock"
	"github.com/D4rk4/yago/yagonode/internal/peermessage"
	"github.com/D4rk4/yago/yagonode/internal/peernews"
	"github.com/D4rk4/yago/yagonode/internal/peerprofile"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/seedlist"
	"github.com/D4rk4/yago/yagonode/internal/sharedblacklist"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type peerExchange struct {
	router                       httpguard.WireRouter
	identity                     nodeidentity.Identity
	report                       nodestatus.Report
	config                       nodeConfig
	vault                        *vault.Vault
	client                       *http.Client
	peer                         *metrics.PeerMetrics
	host                         hostlinks.IncomingHostLinks
	roster                       peerroster.Roster
	news                         *peernews.Pool
	externalReachabilityEvidence *peerannouncement.ExternalReachabilityEvidence
}

type peerExchangeRuntime struct {
	announcer                    peerannouncement.Announcer
	externalReachabilityEvidence *peerannouncement.ExternalReachabilityEvidence
}

var (
	openPeerRoster  = peerroster.Open
	openPeerMailbox = peermessage.OpenMailbox
)

func openObservedPeerRoster(
	ctx context.Context,
	vault *vault.Vault,
	self yagomodel.Hash,
	peer *metrics.PeerMetrics,
) (peerroster.Roster, error) {
	roster, err := openPeerRoster(
		ctx, vault, self, time.Now,
		peerroster.Capacity{Reservoir: reservoirCapacity, Active: activeSetCapacity},
	)
	if err != nil {
		return nil, fmt.Errorf("open peer roster: %w", err)
	}
	var observer peerMetricsObserver
	if peer != nil {
		observer = peer
	}

	return observePeerRoster(ctx, roster, observer), nil
}

type nodeStatusSources struct {
	storage        nodeStorage
	roster         peerroster.Roster
	news           *peernews.Pool
	tally          *transfertally.Tally
	classification nodestatus.PublishedPeerClassification
	queues         nodestatus.SeedQueueStatisticsSource
}

func newNodeStatusReport(
	identity nodeidentity.Identity,
	sources nodeStatusSources,
) nodestatus.Report {
	return nodestatus.NewReport(identity, nodestatus.ReportSources{
		RWI:                sources.storage.postings,
		URLs:               sources.storage.urlDirectory,
		Peers:              sources.roster,
		News:               sources.news,
		Transfers:          reportedTransferTally{tally: sources.tally},
		PeerClassification: sources.classification,
		Queues:             sources.queues,
	})
}

func openPeerStores(
	ctx context.Context,
	vault *vault.Vault,
	self yagomodel.Hash,
	peer *metrics.PeerMetrics,
) (peerroster.Roster, *peernews.Pool, *transfertally.Tally, *peerblock.Store, error) {
	roster, err := openObservedPeerRoster(ctx, vault, self, peer)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	news, err := openRuntimePeerNews(vault, time.Now)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open peer news: %w", err)
	}
	tally, err := openRuntimeTransferTally(vault)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open transfer tally: %w", err)
	}
	blocks, err := peerblock.Open(vault, time.Now)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open peer blocklist: %w", err)
	}

	// Wrap the roster so blocked peers vanish from the reachable set that feeds
	// index fan-out and the peer lists this node advertises.
	return newBlockingRoster(roster, blocks), news, tally, blocks, nil
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
			Status:        peeringStatus{report: p.report, networkName: p.config.NetworkName},
			Reachability:  helloPeerRoster{roster: p.roster},
			Client:        p.client,
			News:          p.news,
			PreferHTTPS:   p.config.PeerHTTPSPreferred,
			NetworkAccess: p.identity.NetworkAccess(),
		},
	)
	seedlist.Mount(p.router, p.report, newNodeSeedlistDirectory(p.roster))
	hostlinks.Mount(p.router, p.identity, p.report, p.host)
	peermessage.Mount(p.router, p.identity, mailbox)
	peerprofile.Mount(p.router, p.identity, peerprofile.NewProfileFile(p.config.DataDir))
	sharedblacklist.Mount(
		p.router,
		p.identity,
		sharedblacklist.NewFileBlacklists(p.config.DataDir),
	)

	return peerExchangeRuntime{
		announcer: peerannouncement.New(
			peerannouncement.Config{
				Client:                       p.client,
				NetworkName:                  p.config.NetworkName,
				Interval:                     p.config.AnnounceInterval,
				GreetsPerCycle:               p.config.GreetsPerCycle,
				Observer:                     announceObserver,
				News:                         p.news,
				PreferHTTPS:                  p.config.PeerHTTPSPreferred,
				NetworkAccess:                p.identity.NetworkAccess(),
				ExternalReachabilityEvidence: p.externalReachabilityEvidence,
				AdmitExternalObserverAddress: yagoegress.NewGuard(false).AdmitAddr,
			},
			p.report,
			bootstrap.NewObserved(p.client, p.config.SeedlistURLs, seedObserver),
			p.roster,
		),
		externalReachabilityEvidence: p.externalReachabilityEvidence,
	}, nil
}
