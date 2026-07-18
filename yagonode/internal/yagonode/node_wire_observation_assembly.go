package yagonode

import (
	"net/http"

	"github.com/D4rk4/yago/yagonode/internal/httpguard"
	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagonode/internal/nodestatus"
	"github.com/D4rk4/yago/yagonode/internal/transfertally"
)

type nodeWireObservation struct {
	report nodestatus.Report
	tally  *transfertally.Tally
}

func mountDHTObservedNodeWire(
	config nodeConfig,
	identity nodeidentity.Identity,
	storage nodeStorage,
	telemetry nodeTelemetry,
	observation nodeWireObservation,
) (*http.ServeMux, httpguard.WireRouter) {
	wireStorage := observeDHTInboundStorage(
		storage,
		telemetry.dhtInbound,
		observation.tally,
	)
	mux, router := newNodeWireMux(config, observation.report)
	mountNodeWireHandlers(router, identity, wireStorage, telemetry.saturation, config)

	return mux, router
}
