package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/snippetmark"
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

	response, err := s.searcher.Search(ctx, searchcore.Request{
		Query:  query.Query,
		Source: source,
		Offset: query.Offset,
		Limit:  query.Limit,
	})
	if err != nil {
		return adminui.SearchResults{}, fmt.Errorf("admin search: %w", err)
	}

	return adminui.SearchResults{
		Query:        query.Query,
		Global:       query.Global,
		TotalResults: response.TotalResults,
		Results:      adminSearchResults(response.Results, response.Request.Terms),
		Failures:     adminSearchFailures(response.PartialFailures),
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
			SnippetHTML: snippetmark.Highlight(result.Snippet, terms),
			Host:        result.Host,
			Date:        result.Date,
			SizeName:    resultSizeName(result.Size),
			Source:      resultProvenance(result),
		})
	}

	return rendered
}

func adminSearchFailures(failures []searchcore.PartialFailure) []string {
	rendered := make([]string, 0, len(failures))
	for _, failure := range failures {
		rendered = append(rendered, failure.Source+": "+failure.Reason)
	}

	return rendered
}
