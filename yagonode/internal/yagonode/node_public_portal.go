package yagonode

import (
	"context"
	"fmt"
	"strconv"

	"github.com/D4rk4/yago/yagonode/internal/cachedpage"
	"github.com/D4rk4/yago/yagonode/internal/faviconproxy"
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

// cachedCopyURL links the node's stored copy for locally stored results; peer
// and web-fallback hits have no stored page to show. Local hits in a global
// search carry SourceGlobal, so this goes through Result.StoredLocally.
func cachedCopyURL(result searchcore.Result) string {
	if !result.StoredLocally() {
		return ""
	}

	return cachedpage.URLFor(result.URL)
}

// resultFaviconURL links the result host's icon through this node's favicon
// proxy, so origin hosts never see the searcher before a click.
func resultFaviconURL(result searchcore.Result) string {
	if result.Host == "" {
		return ""
	}

	return faviconproxy.URLFor(result.Host)
}

// resultProvenance labels where a hit came from for the transparency badge.
func resultProvenance(result searchcore.Result) string {
	switch {
	case result.FromWeb():
		return "web"
	case result.FromPeer():
		return "peer"
	default:
		return "local"
	}
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

	out := publicportal.SearchResults{Query: query, TotalResults: response.TotalResults}
	out.Results = make([]publicportal.SearchResult, 0, len(response.Results))
	for _, result := range response.Results {
		provenance := resultProvenance(result)
		switch provenance {
		case "web":
			out.WebCount++
		case "peer":
			out.PeerCount++
		default:
			out.LocalCount++
		}
		out.Results = append(out.Results, publicportal.SearchResult{
			Title:       result.Title,
			URL:         result.URL,
			DisplayURL:  result.DisplayURL,
			Snippet:     result.Snippet,
			SnippetHTML: snippetmark.Highlight(result.Snippet, response.Request.Terms),
			Host:        result.Host,
			Date:        result.DisplayDate(),
			SizeName:    resultSizeName(result.Size),
			Marked:      result.FromWeb(),
			CachedURL:   cachedCopyURL(result),
			Provenance:  provenance,
			FaviconURL:  resultFaviconURL(result),
		})
	}

	return out, nil
}
