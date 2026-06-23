package search

import (
	"context"
	"fmt"
	"log/slog"
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
	peer     httpguard.PeerIdentity
	searcher searcher
}

func (e searchEndpoint) Serve(
	ctx context.Context,
	req yacyproto.SearchRequest,
) (yacyproto.SearchResponse, error) {
	resp := yacyproto.SearchResponse{}

	if e.peer.NetworkMatches(req.NetworkName) {
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
			return yacyproto.SearchResponse{}, fmt.Errorf("search: %w", err)
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

	return resp, nil
}

func queryFromRequest(req yacyproto.SearchRequest) searchQuery {
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

	return searchQuery{
		Words:       req.Query,
		Exclude:     req.Exclude,
		URLs:        req.URLs,
		MaxResults:  count,
		MaxDistance: req.MaxDist,
		MaxTime:     maxTime,
		Abstracts:   abstractRequest(req),
		searchFilters: searchFilters{
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

func abstractRequest(req yacyproto.SearchRequest) abstractSpec {
	switch req.Abstracts {
	case "":
		return abstractSpec{Mode: abstractNone}
	case yacyproto.SearchAbstractsAuto:
		return abstractSpec{Mode: abstractAuto}
	default:
		return abstractSpec{
			Mode:  abstractExplicit,
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
