package api

import (
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type transferURLHandler struct {
	guard    RequestGuard
	status   contracts.RuntimeStatus
	receiver contracts.URLReceiver
}

func NewTransferURLHandler(
	guard RequestGuard,
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

	req, err := yacyproto.ParseTransferURLRequest(ctx, form)
	if err != nil {
		failBadRequest(ctx, w, err)

		return
	}

	resp := yacyproto.TransferURLResponse{
		ResponseHeader: responseHeader(h.status.Snapshot(ctx)),
	}

	if !h.guard.networkMatches(form) || !h.guard.youAreMatches(req.YouAre) {
		resp.Result = yacyproto.ResultWrongTarget
		writeWireMessage(ctx, w, resp.Encode())

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

	slog.DebugContext(
		ctx,
		"transfer url stored",
		slog.Bool("busy", receipt.Busy),
		slog.Int("doubleCount", receipt.Double),
		slog.Int("errorUrlCount", len(receipt.ErrorURL)),
	)
	writeWireMessage(ctx, w, resp.Encode())
}
