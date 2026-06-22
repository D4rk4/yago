package main

import (
	"fmt"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/bootstrap"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/crawling"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/landing"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodestatus"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/peering"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/search"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type node struct {
	peerMux   *http.ServeMux
	sweeper   eviction.Sweeper
	announcer bootstrap.Module
}

func assembleNode(
	config nodeConfig,
	settings bootstrap.BootstrapSettings,
	vault *boltvault.Vault,
	client *http.Client,
) (node, error) {
	guard := httpguard.NewRequestGuard(
		httpguard.LocalPeer{Hash: config.Hash, NetworkName: config.NetworkName},
		httpguard.DefaultMaxBodyBytes,
		httpguard.DefaultRequestTimeout,
	)

	holder := &reportHolder{}

	urlModule, err := urlmeta.New(vault, guard, urlmetaStatus{holder})
	if err != nil {
		return node{}, fmt.Errorf("urlmeta module: %w", err)
	}

	rwiModule, err := rwi.New(
		vault,
		guard,
		rwiStatus{holder},
		urlModule.Directory,
		rwi.Config{BatchCap: receiveBatchCap, PauseSeconds: receiveBusyPauseSecs},
	)
	if err != nil {
		return node{}, fmt.Errorf("rwi module: %w", err)
	}

	statusModule := nodestatus.New(
		nodeIdentity(config),
		guard,
		rwiModule.Directory,
		urlModule.Directory,
	)
	holder.report = statusModule.Report

	searchModule := search.New(
		guard,
		searchStatus{holder},
		rwiModule.Index,
		urlModule.Directory,
		searchPostingsPerWord,
	)
	peeringModule := peering.New(
		guard,
		peeringStatus{holder: holder, networkName: config.NetworkName},
		client,
		trustedSeedCapacity,
		config.TrustedProxies,
	)
	crawlingModule := crawling.New(guard, crawlingStatus{holder})
	landingModule := landing.New()

	sweeper := eviction.New(
		vault,
		rwiModule.Directory,
		urlModule.Evictor,
		eviction.Config{TargetFraction: evictionTargetFraction, BatchSize: evictionBatch},
	)

	announcer := bootstrap.New(
		client,
		config.NetworkName,
		settings,
		bootstrapStatus{holder},
		peeringModule.Registry,
	)

	mux := http.NewServeMux()
	mux.Handle("/{$}", landingModule.Endpoint)
	mux.Handle(yacyproto.PathHello, peeringModule.HelloEndpoint)
	mux.Handle(yacyproto.PathTransferRWI, rwiModule.TransferRWI)
	mux.Handle(yacyproto.PathTransferURL, urlModule.Endpoint)
	mux.Handle(yacyproto.PathSearch, searchModule.Endpoint)
	mux.Handle(yacyproto.PathQuery, statusModule.Query)
	mux.Handle(yacyproto.PathCrawlReceipt, crawlingModule.Endpoint)

	return node{peerMux: mux, sweeper: sweeper, announcer: announcer}, nil
}
