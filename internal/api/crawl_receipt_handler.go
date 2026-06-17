package api

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type crawlReceiptHandler struct {
	guard  requestGuard
	status core.RuntimeStatus
}

func newCrawlReceiptHandler(guard requestGuard, status core.RuntimeStatus) *crawlReceiptHandler {
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

	writeWireMessage(w, resp.Encode())
}
