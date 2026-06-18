package api

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type transferURLHandler struct {
	guard    requestGuard
	status   contracts.RuntimeStatus
	receiver contracts.URLReceiver
}

func newTransferURLHandler(
	guard requestGuard,
	status contracts.RuntimeStatus,
	receiver contracts.URLReceiver,
) *transferURLHandler {
	return &transferURLHandler{guard: guard, status: status, receiver: receiver}
}

func (h *transferURLHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	form, ctx, cancel, ok := h.guard.parse(w, r, yacyproto.TransferURLEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	req, err := yacyproto.ParseTransferURLRequest(form)
	if err != nil {
		failBadRequest(ctx, w, err)

		return
	}

	resp := yacyproto.TransferURLResponse{
		ResponseHeader: responseHeader(h.status.Snapshot(ctx)),
	}

	if !h.guard.networkMatches(form) || !h.guard.youAreMatches(req.YouAre) {
		resp.Result = yacyproto.ResultWrongTarget
		writeWireMessage(w, resp.Encode())

		return
	}

	receipt, err := h.receiver.ReceiveURLs(ctx, req.URLs)
	if err != nil {
		failInternal(ctx, w, "receive failed", err)

		return
	}

	if receipt.Busy {
		resp.Result = yacyproto.ResultErrorNotGranted
	} else {
		resp.Result = yacyproto.ResultOK
	}
	resp.Double = receipt.Double
	resp.ErrorURL = receipt.ErrorURL

	writeWireMessage(w, resp.Encode())
}
