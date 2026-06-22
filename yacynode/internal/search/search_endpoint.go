package search

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/httpguard"
	"github.com/nikitakarpei/yacy-rwi-node/yacyproto"
)

const (
	defaultSearchCount = 10
	defaultSearchTime  = 3 * time.Second
)

type searchEndpoint struct {
	guard    httpguard.RequestGuard
	respond  httpguard.WireResponder
	searcher searcher
}

func (e searchEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	form, ctx, cancel, ok := e.guard.Parse(w, r, yacyproto.SearchEndpointMethods)
	if !ok {
		return
	}
	defer cancel()

	req, err := yacyproto.ParseSearchRequest(ctx, form)
	if err != nil {
		httpguard.FailBadRequest(ctx, w, err)

		return
	}

	resp := yacyproto.SearchResponse{}

	if e.guard.NetworkMatches(form) {
		query := queryFromRequest(req)
		if ignored := ignoredOptionNames(query); len(ignored) != 0 {
			slog.DebugContext(ctx, "ignoring accepted search options",
				slog.Any("options", ignored),
			)
		}
		searchCtx := ctx
		if query.MaxTime > 0 {
			var searchCancel func()
			searchCtx, searchCancel = context.WithTimeout(ctx, query.MaxTime)
			defer searchCancel()
		}

		result, err := e.searcher.Search(searchCtx, query)
		if err != nil {
			httpguard.FailInternal(ctx, w, "search failed", err)

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

	slog.DebugContext(ctx, "search completed",
		slog.Int("resultCount", resp.Count),
		slog.Int("joinCount", resp.JoinCount),
	)
	e.respond.Write(ctx, w, resp.Encode())
}

func queryFromRequest(req yacyproto.SearchRequest) Query {
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

	return Query{
		Words:       req.Query,
		Exclude:     req.Exclude,
		URLs:        req.URLs,
		MaxResults:  count,
		MaxDistance: req.MaxDist,
		MaxTime:     maxTime,
		Abstracts:   abstractRequest(req),
		Filters: Filters{
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

func abstractRequest(req yacyproto.SearchRequest) AbstractRequest {
	switch req.Abstracts {
	case "":
		return AbstractRequest{Mode: AbstractNone}
	case yacyproto.SearchAbstractsAuto:
		return AbstractRequest{Mode: AbstractAuto}
	default:
		return AbstractRequest{
			Mode:  AbstractExplicit,
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
