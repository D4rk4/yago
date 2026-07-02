package main

import (
	"context"
	"net/http"

	"github.com/D4rk4/yago/yacynode/internal/crawling"
	"github.com/D4rk4/yago/yacynode/internal/crawlurls"
	"github.com/D4rk4/yago/yacynode/internal/documentsearch"
	"github.com/D4rk4/yago/yacynode/internal/eviction"
	"github.com/D4rk4/yago/yacynode/internal/httpguard"
	"github.com/D4rk4/yago/yacynode/internal/landing"
	"github.com/D4rk4/yago/yacynode/internal/nodeidentity"
	"github.com/D4rk4/yago/yacynode/internal/nodestatus"
	"github.com/D4rk4/yago/yacynode/internal/peerannouncement"
	"github.com/D4rk4/yago/yacynode/internal/rwi"
	"github.com/D4rk4/yago/yacynode/internal/urlmeta"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type node struct {
	peerMux   *http.ServeMux
	sweeper   eviction.Sweeper
	announcer peerannouncement.Announcer
	crawl     crawlProcess
}

var (
	openRuntimeNodeStorage      = openNodeStorage
	assembleRuntimePeerExchange = func(exchange peerExchange) (peerannouncement.Announcer, error) {
		return exchange.assemble()
	}
	buildRuntimeCrawl = func(
		ctx context.Context,
		config crawlConfig,
		identity nodeidentity.Identity,
		storage nodeStorage,
	) (crawlProcess, error) {
		runtime, err := buildCrawlRuntime(ctx, config, identity, storage)
		if runtime == nil || err != nil {
			return nil, err
		}

		return runtime, nil
	}
)

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

	storage, err := openRuntimeNodeStorage(vault)
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

	announcer, err := assembleRuntimePeerExchange(peerExchange{
		router:   router,
		identity: identity,
		report:   report,
		config:   config,
		vault:    vault,
		client:   client,
	})
	if err != nil {
		return node{}, err
	}

	crawling.MountCrawlReceipt(router)
	crawlurls.Mount(router, identity, storage.urlDirectory, crawlurls.NoRemoteCrawlURLs{})

	sweeper := newStorageSweeper(vault, storage)

	runtime, err := buildRuntimeCrawl(ctx, config.Crawl, identity, storage)
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
