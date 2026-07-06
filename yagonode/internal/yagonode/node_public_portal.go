package yagonode

import (
	"context"
	"fmt"
	"net/url"
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

// portalImages proxies the page's extracted images for the image grid, so
// pictured hosts never see the searcher before a click.
func portalImages(result searchcore.Result) []publicportal.ResultImage {
	images := make([]publicportal.ResultImage, 0, len(result.Images))
	for _, image := range result.Images {
		images = append(images, publicportal.ResultImage{
			ProxyURL: faviconproxy.ImageURLFor(image.URL),
			Alt:      image.Alt,
			PageURL:  result.URL,
		})
	}

	return images
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
	query, dom string,
	offset, limit int,
) (publicportal.SearchResults, error) {
	domain := searchcore.ContentDomainText
	if dom != "" {
		domain = searchcore.ContentDomain(dom)
	}
	response, err := s.searcher.Search(ctx, searchcore.Request{
		Query:         query,
		Source:        searchcore.SourceGlobal,
		ContentDomain: domain,
		Offset:        offset,
		Limit:         limit,
		WithFacets:    true,
	})
	if err != nil {
		return publicportal.SearchResults{}, fmt.Errorf("portal search: %w", err)
	}

	out := publicportal.SearchResults{Query: query, TotalResults: response.TotalResults}
	if response.Recovered != "" {
		out.Recovered = true
		out.DidYouMean = response.DidYouMean
		if response.DidYouMean != "" {
			out.DidYouMeanURL = "/?q=" + url.QueryEscape(response.DidYouMean)
		}
	}
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
			Images:      portalImages(result),
		})
	}

	out.Facets = portalFacets(query, response.Facets)

	return out, nil
}

// facetOperators maps facet dimensions onto the query operators a click adds;
// dimensions without an operator render as plain counts.
var facetOperators = map[string]string{
	"host":     "site:",
	"filetype": "filetype:",
	"language": "language:",
	"author":   "author:",
}

var facetTitles = map[string]string{
	"host":     "Host",
	"filetype": "File type",
	"language": "Language",
	"author":   "Author",
	"protocol": "Protocol",
	"month":    "Month",
}

// portalFacets renders the local facet groups as sidebar filters: clicking a
// value re-runs the query with the matching operator appended.
func portalFacets(query string, groups []searchcore.FacetGroup) []publicportal.FacetGroup {
	out := make([]publicportal.FacetGroup, 0, len(groups))
	for _, group := range groups {
		items := make([]publicportal.FacetItem, 0, len(group.Terms))
		for _, term := range group.Terms {
			item := publicportal.FacetItem{Label: term.Term, Count: term.Count}
			if operator, ok := facetOperators[group.Name]; ok {
				item.URL = "/?q=" + url.QueryEscape(query+" "+operator+term.Term)
			}
			items = append(items, item)
		}
		out = append(out, publicportal.FacetGroup{Title: facetTitles[group.Name], Items: items})
	}

	return out
}
