package main

import (
	"fmt"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/bootstrap"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/crawling"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/documentsearch"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/landing"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodestatus"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/peering"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
)

type node struct {
	peerMux   *http.ServeMux
	sweeper   eviction.Sweeper
	announcer bootstrap.Announcer
}

func assembleNode(
	config nodeConfig,
	settings bootstrap.BootstrapSettings,
	vault *boltvault.Vault,
	client *http.Client,
) (node, error) {
	guard := httpguard.NewRequestGuard(
		httpguard.DefaultMaxBodyBytes,
		httpguard.DefaultRequestTimeout,
	)
	peer := httpguard.PeerIdentity{Hash: config.Hash, NetworkName: config.NetworkName}

	urlDirectory, urlEvictor, urlReceiver, err := urlmeta.Open(vault)
	if err != nil {
		return node{}, fmt.Errorf("urlmeta storage: %w", err)
	}

	postings, postingReceiver, err := rwi.Open(
		vault,
		urlDirectory,
		rwi.Config{BatchCap: receiveBatchCap, PauseSeconds: receiveBusyPauseSecs},
	)
	if err != nil {
		return node{}, fmt.Errorf("rwi storage: %w", err)
	}

	report := nodestatus.NewReport(nodeIdentity(config), postings, urlDirectory)

	gate := httpguard.WireGate{
		Guard:   guard,
		Respond: httpguard.NewWireResponder(report),
		Address: httpguard.NewClientAddressResolver(config.TrustedProxies),
	}

	mux := http.NewServeMux()
	mux.Handle("/{$}", landing.NewEndpoint())
	router := httpguard.NewWireRouter(mux, gate)

	urlmeta.MountTransferURL(router, peer, urlReceiver)
	rwi.MountTransferRWI(router, peer, postingReceiver)
	nodestatus.MountQuery(router, peer, postings, urlDirectory)

	documentsearch.MountSearch(router, peer, postings, urlDirectory, searchPostingsPerWord)

	registry := peering.NewTrustedSeeds(trustedSeedCapacity)
	peering.MountHello(
		router,
		peer,
		peeringStatus{report: report, networkName: config.NetworkName},
		registry,
		client,
	)

	crawling.MountCrawlReceipt(router)

	sweeper := eviction.NewSweeper(
		vault,
		postings,
		urlEvictor,
		eviction.Config{TargetFraction: evictionTargetFraction, BatchSize: evictionBatch},
	)

	announcer := bootstrap.NewAnnouncer(
		client,
		config.NetworkName,
		settings,
		report,
		registry,
	)

	return node{peerMux: mux, sweeper: sweeper, announcer: announcer}, nil
}
