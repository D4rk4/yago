package main

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/api"
	"github.com/nikitakarpei/yacy-rwi-node/internal/core/services"
	"github.com/nikitakarpei/yacy-rwi-node/internal/infrastructure"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

func newPeerProtocolMux(
	config infrastructure.NodeConfig,
	status services.RuntimeStatus,
	peers *services.PeerDirectory,
	storage *infrastructure.BboltStorage,
	sweeper *services.RWIEvictionSweeper,
) *http.ServeMux {
	guard := api.NewRequestGuard(
		nodeIdentity(config),
		api.DefaultMaxBodyBytes,
		api.DefaultRequestTimeout,
	)
	rwiReceiver := services.NewRWIReceiver(
		storage,
		storage,
		receiveBatchCap,
		receiveBusyPauseSecs,
		services.WithEvictionTrigger(sweeper.Trigger),
	)
	urlReceiver := services.NewURLReceiver(
		storage,
		services.WithURLEvictionTrigger(sweeper.Trigger),
	)

	mux := http.NewServeMux()
	mux.Handle("/{$}", api.NewLandingPageHandler())
	mux.Handle(
		yacyproto.PathHello,
		api.NewHelloHandler(guard, status, peers, config.TrustedProxies),
	)
	mux.Handle(yacyproto.PathTransferRWI, api.NewTransferRWIHandler(guard, status, rwiReceiver))
	mux.Handle(yacyproto.PathTransferURL, api.NewTransferURLHandler(guard, status, urlReceiver))
	mux.Handle(
		yacyproto.PathSearch,
		api.NewSearchHandler(
			guard,
			status,
			services.NewSearcher(storage, storage, searchPostingsPerWord),
		),
	)
	mux.Handle(
		yacyproto.PathQuery,
		api.NewQueryHandler(guard, status, services.NewCounter(storage, storage)),
	)
	mux.Handle(yacyproto.PathCrawlReceipt, api.NewCrawlReceiptHandler(guard, status))

	return mux
}
