package main

import (
	"context"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/crawling"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/documentsearch"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/eviction"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/landing"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/nodestatus"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/peerannouncement"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/rwi"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/urlmeta"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

type node struct {
	peerMux   *http.ServeMux
	sweeper   eviction.Sweeper
	announcer peerannouncement.Announcer
	crawl     *crawlRuntime
}

func assembleNode(
	ctx context.Context,
	config nodeConfig,
	vault *vault.Vault,
	client *http.Client,
) (node, error) {
	guard := httpguard.NewRequestGuard(
		httpguard.DefaultMaxBodyBytes,
		httpguard.DefaultRequestTimeout,
	)
	identity := nodeIdentity(config)

	storage, err := openNodeStorage(vault)
	if err != nil {
		return node{}, err
	}

	report := nodestatus.NewReport(identity, storage.postings, storage.urlDirectory)

	gate := httpguard.WireGate{
		Guard:   guard,
		Respond: httpguard.NewWireResponder(report),
		Address: httpguard.NewClientAddressResolver(config.TrustedProxies),
	}

	mux := http.NewServeMux()
	mux.Handle("/{$}", landing.NewEndpoint())
	router := httpguard.NewWireRouter(mux, gate)

	urlmeta.MountTransferURL(router, identity, storage.urlReceiver)
	rwi.MountTransferRWI(router, identity, storage.postingReceiver)
	nodestatus.MountQuery(
		router,
		identity,
		storage.postings,
		storage.references,
		storage.urlDirectory,
	)

	documentsearch.MountSearch(
		router,
		identity,
		storage.postings,
		storage.urlDirectory,
		searchPostingsPerWord,
	)

	announcer, err := peerExchange{
		router:   router,
		identity: identity,
		report:   report,
		config:   config,
		vault:    vault,
		client:   client,
	}.assemble()
	if err != nil {
		return node{}, err
	}

	crawling.MountCrawlReceipt(router)

	sweeper := newStorageSweeper(vault, storage)

	runtime, err := buildCrawlRuntime(ctx, config.Crawl, identity, storage)
	if err != nil {
		return node{}, err
	}

	return node{
		peerMux:   mux,
		sweeper:   sweeper,
		announcer: announcer,
		crawl:     runtime,
	}, nil
}

func newStorageSweeper(vault *vault.Vault, storage nodeStorage) eviction.Sweeper {
	return eviction.NewSweeper(
		vault,
		storage.postingPurger,
		storage.references,
		storage.urlEvictor,
		storage.staleness,
		eviction.Config{TargetFraction: evictionTargetFraction, BatchSize: evictionBatch},
	)
}
