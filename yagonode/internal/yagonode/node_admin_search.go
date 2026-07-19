package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/modifierhint"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type searchSource struct {
	searcher searchcore.Searcher
}

func newSearchSource(searcher searchcore.Searcher) searchSource {
	return searchSource{searcher: searcher}
}

func (s searchSource) Search(
	ctx context.Context,
	query adminui.SearchQuery,
) (adminui.SearchResults, error) {
	source := searchcore.SourceLocal
	if query.Global {
		source = searchcore.SourceGlobal
	}

	request, err := searchcore.NormalizePublicRequest(searchcore.Request{
		Query:         query.Query,
		Source:        source,
		Offset:        query.Offset,
		Limit:         query.Limit,
		ContentDomain: searchcore.ContentDomain(query.Filters.ContentDomain),
		Language:      query.Filters.Language,
		SiteHost:      query.Filters.SiteHost,
	}, query.Limit)
	if err != nil {
		return adminui.SearchResults{}, fmt.Errorf("admin search request: %w", err)
	}

	response, err := s.searcher.Search(ctx, request)
	if err != nil {
		return adminui.SearchResults{}, fmt.Errorf("admin search: %w", err)
	}

	return adminui.SearchResults{
		Query:        query.Query,
		Global:       query.Global,
		TotalResults: response.TotalResults,
		Results:      adminSearchResults(response.Results, response.Request.Terms),
		Failures:     adminSearchFailures(response.PartialFailures),
		Hint:         modifierhint.Text(response.Request, response.TotalResults),
	}, nil
}

func adminSearchResults(
	results []searchcore.Result,
	terms []string,
) []adminui.SearchResult {
	rendered := make([]adminui.SearchResult, 0, len(results))
	for _, result := range results {
		rendered = append(rendered, adminui.SearchResult{
			Title:       result.Title,
			URL:         result.URL,
			DisplayURL:  result.DisplayURL,
			Snippet:     result.Snippet,
			SnippetHTML: highlightedResultSnippet(result, terms),
			Host:        result.Host,
			Date:        result.DisplayDate(),
			SizeName:    resultSizeName(result.Size),
			Source:      resultProvenance(result),
		})
	}

	return rendered
}

func adminSearchFailures(failures []searchcore.PartialFailure) []string {
	rendered := make([]string, 0, len(failures))
	for _, failure := range failures {
		rendered = append(rendered, failure.SourceLabel()+": "+failure.Reason)
	}

	return rendered
}
