package yagonode

import (
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/documentsearch"
	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/peerroster"
	"github.com/D4rk4/yago/yagonode/internal/remotecrawl"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
)

type nodeWireObservation struct {
	report nodestatus.Report
	tally  *transfertally.Tally
}

type dhtObservedNodeWireInput struct {
	config        nodeConfig
	identity      nodeidentity.Identity
	storage       nodeStorage
	peers         peerroster.Roster
	telemetry     nodeTelemetry
	observation   nodeWireObservation
	remoteCrawl   *remotecrawl.Broker
	peerLifecycle documentsearch.PotentialPeerObserver
}

type nodeWireHandlerAssembly struct {
	router        httpguard.WireRouter
	identity      nodeidentity.Identity
	storage       nodeStorage
	peers         peerroster.Roster
	saturation    *metrics.SaturationMetrics
	config        nodeConfig
	remoteCrawl   *remotecrawl.Broker
	peerLifecycle documentsearch.PotentialPeerObserver
}

func mountDHTObservedNodeWire(
	in dhtObservedNodeWireInput,
) (*http.ServeMux, httpguard.WireRouter) {
	wireStorage := observeDHTInboundStorage(
		in.storage,
		in.telemetry.dhtInbound,
		in.observation.tally,
	)
	mux, router := newNodeWireMux(in.config, in.observation.report)
	mountNodeWireHandlers(nodeWireHandlerAssembly{
		router: router, identity: in.identity, storage: wireStorage,
		peers: in.peers, saturation: in.telemetry.saturation, config: in.config,
		remoteCrawl: in.remoteCrawl, peerLifecycle: in.peerLifecycle,
	})

	return mux, router
}
