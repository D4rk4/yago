package api

import (
	"net/http"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

type searchHandler struct {
	guard    requestGuard
	status   contracts.RuntimeStatus
	searcher contracts.Searcher
}

func newSearchHandler(
	guard requestGuard,
	status contracts.RuntimeStatus,
	searcher contracts.Searcher,
) *searchHandler {
	return &searchHandler{guard: guard, status: status, searcher: searcher}
}

func (h *searchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	form, ctx, cancel, ok := h.guard.parse(w, r, yacyproto.SearchEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	req, err := yacyproto.ParseSearchRequest(form)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)

		return
	}

	resp := yacyproto.SearchResponse{
		ResponseHeader: responseHeader(h.status.Snapshot(ctx)),
	}

	if h.guard.networkMatches(form) {
		result, err := h.searcher.Search(ctx, contracts.SearchQuery{
			Words:       req.Query,
			Exclude:     req.Exclude,
			URLs:        req.URLs,
			MaxResults:  req.Count,
			MaxDistance: req.MaxDist,
		})
		if err != nil {
			http.Error(w, "search failed", http.StatusInternalServerError)

			return
		}

		resp.JoinCount = result.JoinCount
		resp.Count = len(result.Resources)
		resp.Resources = result.Resources
		resp.IndexCount = result.WordCounts
		resp.IndexAbstract = result.Abstracts
	}

	writeWireMessage(w, resp.Encode())
}
