package api

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type queryHandler struct {
	guard   requestGuard
	status  core.RuntimeStatus
	counter core.Counter
}

func newQueryHandler(
	guard requestGuard,
	status core.RuntimeStatus,
	counter core.Counter,
) *queryHandler {
	return &queryHandler{guard: guard, status: status, counter: counter}
}

func (h *queryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	form, ctx, cancel, ok := h.guard.parse(w, r, yacyproto.QueryEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	req, err := yacyproto.ParseQueryRequest(form)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}

	resp := yacyproto.QueryResponse{
		ResponseHeader: responseHeader(h.status.Snapshot(ctx)),
		Response:       yacyproto.QueryResponseRejected,
	}

	kind, supported := countKind(req.Object)
	if supported && h.guard.networkMatches(form) && h.guard.youAreMatches(req.YouAre) {
		count, err := h.counter.Count(ctx, kind)
		if err != nil {
			http.Error(w, "count failed", http.StatusInternalServerError)

			return
		}

		resp.Response = count
	}

	writeWireMessage(w, resp.Encode())
}

func countKind(object yacyproto.QueryObject) (core.CountKind, bool) {
	switch object {
	case yacyproto.ObjectRWICount:
		return core.RWICount, true
	case yacyproto.ObjectRWIURLCount:
		return core.RWIURLCount, true
	case yacyproto.ObjectLURLCount:
		return core.LURLCount, true
	default:
		return 0, false
	}
}
