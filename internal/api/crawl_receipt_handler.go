package api

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type crawlReceiptHandler struct {
	guard  RequestGuard
	status contracts.RuntimeStatus
}

func NewCrawlReceiptHandler(
	guard RequestGuard,
	status contracts.RuntimeStatus,
) *crawlReceiptHandler {
	return &crawlReceiptHandler{guard: guard, status: status}
}

func (h *crawlReceiptHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, ctx, cancel, ok := h.guard.parse(w, r, yacyproto.CrawlReceiptEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	resp := yacyproto.CrawlReceiptResponse{
		ResponseHeader: responseHeader(h.status.Snapshot(ctx)),
	}

	writeWireMessage(ctx, w, resp.Encode())
}
