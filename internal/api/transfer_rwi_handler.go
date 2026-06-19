package api

import (
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type transferRWIHandler struct {
	guard    requestGuard
	status   contracts.RuntimeStatus
	receiver contracts.RWIReceiver
}

func newTransferRWIHandler(
	guard requestGuard,
	status contracts.RuntimeStatus,
	receiver contracts.RWIReceiver,
) *transferRWIHandler {
	return &transferRWIHandler{guard: guard, status: status, receiver: receiver}
}

func (h *transferRWIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	form, ctx, cancel, ok := h.guard.parse(w, r, yacyproto.TransferRWIEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	req, err := yacyproto.ParseTransferRWIRequest(form)
	if err != nil {
		failBadRequest(ctx, w, err)

		return
	}

	resp := yacyproto.TransferRWIResponse{
		ResponseHeader: responseHeader(h.status.Snapshot(ctx)),
	}

	if !h.guard.networkMatches(form) || !h.guard.youAreMatches(req.YouAre) {
		resp.Result = yacyproto.ResultWrongTarget
		writeWireMessage(w, resp.Encode())

		return
	}

	slog.DebugContext(
		ctx,
		"transfer rwi request accepted",
		"word_count", req.WordCount,
		"entry_count", req.EntryCount,
		"accepted_entry_count", len(req.Indexes),
	)

	receipt, err := h.receiver.ReceiveRWI(ctx, req.Indexes)
	if err != nil {
		failInternal(ctx, w, "receive failed", err)

		return
	}

	if receipt.Busy {
		resp.Result = yacyproto.ResultBusy
	} else {
		resp.Result = yacyproto.ResultOK
	}
	resp.Pause = receipt.Pause
	resp.UnknownURL = receipt.UnknownURL
	resp.ErrorURL = receipt.ErrorURL

	slog.DebugContext(
		ctx,
		"transfer rwi stored",
		"busy", receipt.Busy,
		"unknown_url_count", len(receipt.UnknownURL),
		"error_url_count", len(receipt.ErrorURL),
	)
	writeWireMessage(w, resp.Encode())
}
