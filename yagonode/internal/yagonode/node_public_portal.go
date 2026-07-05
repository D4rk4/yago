package yagonode

import (
	"context"
	"fmt"
	"strconv"

	"github.com/D4rk4/yago/yagonode/internal/cachedpage"
	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/snippetmark"
)

type portalSource struct {
	searcher searchcore.Searcher
}

// resultSizeName renders a stored page size for the human surfaces, empty
// when the size is unknown.
func resultSizeName(size int) string {
	if size <= 0 {
		return ""
	}

	return strconv.Itoa(size) + " bytes"
}

func newPortalSource(searcher searchcore.Searcher) portalSource {
	return portalSource{searcher: searcher}
}

// cachedCopyURL links the node's stored copy for locally indexed results; other
// sources (peers, web fallback) have no stored page to show.
func cachedCopyURL(result searchcore.Result) string {
	if result.Source != searchcore.SourceLocal {
		return ""
	}

	return cachedpage.URLFor(result.URL)
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
			Title:       result.Title,
			URL:         result.URL,
			DisplayURL:  result.DisplayURL,
			Snippet:     result.Snippet,
			SnippetHTML: snippetmark.Highlight(result.Snippet, response.Request.Terms),
			Host:        result.Host,
			Date:        result.Date,
			SizeName:    resultSizeName(result.Size),
			Marked:      result.Source == searchcore.SourceWeb,
			CachedURL:   cachedCopyURL(result),
		})
	}

	return publicportal.SearchResults{
		Query:        query,
		TotalResults: response.TotalResults,
		Results:      results,
	}, nil
}
