package api

import (
	"net"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type helloHandler struct {
	guard          requestGuard
	status         contracts.RuntimeStatus
	peers          contracts.PeerDirectory
	trustedProxies []*net.IPNet
}

func newHelloHandler(
	guard requestGuard,
	status contracts.RuntimeStatus,
	peers contracts.PeerDirectory,
	trustedProxies []*net.IPNet,
) *helloHandler {
	return &helloHandler{
		guard:          guard,
		status:         status,
		peers:          peers,
		trustedProxies: trustedProxies,
	}
}

func (h *helloHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	form, ctx, cancel, ok := h.guard.parse(w, r, yacyproto.HelloEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	req, err := yacyproto.ParseHelloRequest(form)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}

	snapshot := h.status.Snapshot(ctx)
	resp := yacyproto.HelloResponse{
		ResponseHeader: responseHeader(snapshot),
		YourIP:         clientAddress(r, h.trustedProxies),
		Seeds:          []yacymodel.Seed{snapshot.Seed},
	}

	if h.guard.networkMatches(form) {
		outcome, err := h.peers.Hello(ctx, req.Seed, req.Count)
		if err != nil {
			http.Error(w, "hello failed", http.StatusInternalServerError)

			return
		}

		resp.YourType = outcome.CallerType
		resp.Seeds = append(resp.Seeds, outcome.Known...)
	}

	writeWireMessage(w, resp.Encode())
}
