package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const (
	defaultSearchCount = 10
	defaultSearchTime  = 3 * time.Second
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
		failBadRequest(ctx, w, err)

		return
	}

	resp := yacyproto.SearchResponse{
		ResponseHeader: responseHeader(h.status.Snapshot(ctx)),
	}

	if h.guard.networkMatches(form) {
		query := searchQueryFromRequest(req)
		if unsupported := contracts.UnsupportedSearchOptions(query); len(unsupported) != 0 {
			slog.DebugContext(ctx, "unsupported search option", "options", unsupported)
			http.Error(w, "unsupported search option", http.StatusBadRequest)

			return
		}
		if ignored := contracts.IgnoredSearchOptions(query); len(ignored) != 0 {
			slog.DebugContext(ctx, "ignoring accepted search options", "options", ignored)
		}
		searchCtx := ctx
		var searchCancel func()
		if query.MaxTime > 0 {
			searchCtx, searchCancel = context.WithTimeout(ctx, query.MaxTime)
			defer searchCancel()
		}

		result, err := h.searcher.Search(searchCtx, query)
		if err != nil {
			failInternal(ctx, w, "search failed", err)

			return
		}

		resp.SearchTime = int(result.SearchTime / time.Millisecond)
		resp.References = joinReferences(result.References)
		resp.JoinCount = result.JoinCount
		resp.Count = len(result.Resources)
		resp.Resources = result.Resources
		resp.IndexCount = result.WordCounts
		resp.IndexAbstract = result.Abstracts
	}

	slog.DebugContext(
		ctx,
		"search completed",
		"result_count", resp.Count,
		"join_count", resp.JoinCount,
	)
	writeWireMessage(w, resp.Encode())
}

func searchQueryFromRequest(req yacyproto.SearchRequest) contracts.SearchQuery {
	count := req.Count
	if count <= 0 {
		count = defaultSearchCount
	}
	maxTime := time.Duration(req.Time) * time.Millisecond
	if maxTime <= 0 {
		maxTime = defaultSearchTime
	}
	contentDomain := ""
	if req.ContentDom != "" && req.ContentDom != yacyproto.ContentDomainAll {
		contentDomain = string(req.ContentDom)
	}
	filter := req.Filter
	if filter == ".*" {
		filter = ""
	}

	return contracts.SearchQuery{
		Words:       req.Query,
		Exclude:     req.Exclude,
		URLs:        req.URLs,
		MaxResults:  count,
		MaxDistance: req.MaxDist,
		MaxTime:     maxTime,
		Abstracts:   searchAbstractRequest(req),
		Filters: contracts.SearchFilters{
			ContentDomain:    contentDomain,
			StrictContentDom: req.StrictContentDom,
			TimezoneOffset:   req.TimezoneOffset,
			Language:         req.Language,
			Modifier:         req.Modifier,
			Prefer:           req.Prefer,
			Filter:           filter,
			Constraint:       req.Constraint,
			Profile:          req.Profile,
			SiteHost:         req.SiteHost,
			SiteHash:         req.SiteHash,
			Author:           req.Author,
			Collection:       req.Collection,
			FileType:         req.FileType,
			Protocol:         req.Protocol,
			Partitions:       req.Partitions,
		},
	}
}

func searchAbstractRequest(req yacyproto.SearchRequest) contracts.SearchAbstractRequest {
	switch req.Abstracts {
	case "":
		return contracts.SearchAbstractRequest{Mode: contracts.SearchAbstractNone}
	case yacyproto.SearchAbstractsAuto:
		return contracts.SearchAbstractRequest{Mode: contracts.SearchAbstractAuto}
	default:
		return contracts.SearchAbstractRequest{
			Mode:  contracts.SearchAbstractExplicit,
			Words: req.Abstracts.Hashes(),
		}
	}
}

func joinReferences(references []string) string {
	if len(references) == 0 {
		return ""
	}

	return strings.Join(references, ",")
}
