package api

import (
	"log/slog"
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type queryHandler struct {
	guard   requestGuard
	status  contracts.RuntimeStatus
	counter contracts.Counter
}

func newQueryHandler(
	guard requestGuard,
	status contracts.RuntimeStatus,
	counter contracts.Counter,
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
		failBadRequest(ctx, w, err)

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
			failInternal(ctx, w, "count failed", err)

			return
		}

		resp.Response = count
	}

	slog.DebugContext(ctx, "count served", "object", req.Object, "count", resp.Response)
	writeWireMessage(w, resp.Encode())
}

func countKind(object yacyproto.QueryObject) (contracts.CountKind, bool) {
	switch object {
	case yacyproto.ObjectRWICount:
		return contracts.RWICount, true
	case yacyproto.ObjectRWIURLCount:
		return contracts.RWIURLCount, true
	case yacyproto.ObjectLURLCount:
		return contracts.LURLCount, true
	default:
		return 0, false
	}
}
