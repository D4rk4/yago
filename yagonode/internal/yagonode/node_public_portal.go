package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type portalSource struct {
	searcher searchcore.Searcher
}

func newPortalSource(searcher searchcore.Searcher) portalSource {
	return portalSource{searcher: searcher}
}

func (s portalSource) Search(
	ctx context.Context,
	query string,
	offset, limit int,
) (publicportal.SearchResults, error) {
	response, err := s.searcher.Search(ctx, searchcore.Request{
		Query:  query,
		Source: searchcore.SourceGlobal,
		Offset: offset,
		Limit:  limit,
	})
	if err != nil {
		return publicportal.SearchResults{}, fmt.Errorf("portal search: %w", err)
	}

	results := make([]publicportal.SearchResult, 0, len(response.Results))
	for _, result := range response.Results {
		results = append(results, publicportal.SearchResult{
			Title:      result.Title,
			URL:        result.URL,
			DisplayURL: result.DisplayURL,
			Snippet:    result.Snippet,
			Marked:     result.Source == searchcore.SourceWeb,
		})
	}

	return publicportal.SearchResults{
		Query:        query,
		TotalResults: response.TotalResults,
		Results:      results,
	}, nil
}
