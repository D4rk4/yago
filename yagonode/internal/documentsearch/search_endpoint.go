package documentsearch

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/nodeidentity"
	"github.com/D4rk4/yago/yagoproto"
)

const (
	defaultSearchCount = 10
	defaultSearchTime  = 3 * time.Second
)

type searchEndpoint struct {
	identity nodeidentity.Identity
	searcher searcher
}

func (e searchEndpoint) Serve(
	ctx context.Context,
	req yagoproto.SearchRequest,
) (yagoproto.SearchResponse, error) {
	resp := yagoproto.SearchResponse{}

	if e.identity.NetworkMatches(req.NetworkName) {
		criteria, err := searchCriteriaFromRequest(req)
		if err != nil {
			return yagoproto.SearchResponse{}, fmt.Errorf("search criteria: %w", err)
		}
		if ignored := ignoredOptionNames(req); len(ignored) != 0 {
			slog.DebugContext(ctx, "ignoring accepted search options",
				slog.Any("options", ignored),
			)
		}
		searchCtx := ctx
		if criteria.timeLimit > 0 {
			var cancel func()
			searchCtx, cancel = context.WithTimeout(ctx, criteria.timeLimit)
			defer cancel()
		}

		result, err := e.searcher.search(searchCtx, criteria)
		if err != nil {
			return yagoproto.SearchResponse{}, fmt.Errorf("search: %w", err)
		}

		resp.SearchTime = int(result.searchDuration / time.Millisecond)
		resp.References = strings.Join(result.topics, ",")
		resp.JoinCount = result.totalDocumentsMatchingEveryTerm
		resp.Count = len(result.resources)
		resp.Resources = result.resources
		resp.IndexCount = result.totalMatchesPerTerm
		resp.IndexAbstract = result.documentsMatchingEachReportedTerm
	}

	slog.DebugContext(ctx, "search completed",
		slog.Int("resultCount", resp.Count),
		slog.Int("joinCount", resp.JoinCount),
	)

	return resp, nil
}
