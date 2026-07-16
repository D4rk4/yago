package yagonode

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/cachedpage"
	"github.com/D4rk4/yago/yagonode/internal/faviconproxy"
	"github.com/D4rk4/yago/yagonode/internal/modifierhint"
	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
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
		Verify:        searchcore.VerifyIfExist,
		Offset:        offset,
		Limit:         limit,
	})
	if err != nil {
		return publicportal.SearchResults{}, fmt.Errorf("portal search: %w", err)
	}

	out := publicportal.SearchResults{
		Query:                 query,
		TotalResults:          response.TotalResults,
		PeersFailed:           peerSearchFailureTotal(response.PartialFailures),
		FederationUnavailable: federationSearchUnavailable(response.PartialFailures),
		Incomplete:            len(response.PartialFailures) > 0,
		Hint:                  modifierhint.Text(response.Request, response.TotalResults),
	}
	out.DidYouMean = response.DidYouMean
	if response.DidYouMean != "" {
		out.DidYouMeanURL = "/?q=" + url.QueryEscape(response.DidYouMean)
	}
	if response.Recovered != "" {
		out.Recovered = true
	}
	out.Results = make([]publicportal.SearchResult, 0, len(response.Results))
	lexicalPositions := searchcore.LexicalPositions(response.Results, response.Request.Offset)
	for index, result := range response.Results {
		provenance := resultProvenance(result)
		switch provenance {
		case "web":
			out.WebCount++
		case "peer":
			out.PeerCount++
		default:
			out.LocalCount++
		}
		clusterIdentity := result.ClusterID
		if clusterIdentity == "" {
			clusterIdentity = result.URLHash
		}
		if clusterIdentity == "" {
			clusterIdentity = result.URL
		}
		out.Results = append(out.Results, publicportal.SearchResult{
			Title:           result.Title,
			URL:             result.URL,
			DisplayURL:      result.DisplayURL,
			Snippet:         result.Snippet,
			SnippetHTML:     highlightedResultSnippet(result, response.Request.Terms),
			Host:            result.Host,
			Date:            result.DisplayDate(),
			SizeName:        resultSizeName(result.Size),
			CachedURL:       cachedCopyURL(result),
			Provenance:      provenance,
			FaviconURL:      resultFaviconURL(result),
			Images:          portalImages(result),
			URLIdentity:     result.URL,
			ClusterIdentity: clusterIdentity,
			Position:        response.Request.Offset + index + 1,
			LexicalPosition: lexicalPositions[index],
		})
	}

	out.Facets = portalFacetGroups(query, response)

	return out, nil
}

func portalFacetGroups(
	query string,
	response searchcore.Response,
) []publicportal.FacetGroup {
	if len(response.Facets) > 0 {
		return portalFacets(query, response.Facets, "the local corpus")
	}
	if len(response.Results) > 0 {
		return portalFacets(
			query,
			facetsFromResults(response.Results),
			"this visible result window",
		)
	}

	return nil
}

// facetsFromResults tallies facet groups over the result rows themselves —
// the fallback when no corpus counts exist (peer or web answers).
func facetsFromResults(results []searchcore.Result) []searchcore.FacetGroup {
	hosts := map[string]int{}
	languages := map[string]int{}
	for _, result := range results {
		if result.Host != "" {
			hosts[result.Host]++
		}
		if result.Language != "" {
			languages[strings.ToLower(result.Language)]++
		}
	}
	groups := make([]searchcore.FacetGroup, 0, 2)
	if group, ok := facetGroupFromCounts("host", hosts); ok {
		groups = append(groups, group)
	}
	if group, ok := facetGroupFromCounts("language", languages); ok {
		groups = append(groups, group)
	}

	return groups
}

const facetsFromResultsCap = 8

// facetGroupFromCounts orders one tally by count (ties by label) and caps it
// to the sidebar's usual size.
func facetGroupFromCounts(name string, counts map[string]int) (searchcore.FacetGroup, bool) {
	if len(counts) == 0 {
		return searchcore.FacetGroup{}, false
	}
	terms := make([]searchcore.FacetTerm, 0, len(counts))
	for term, count := range counts {
		terms = append(terms, searchcore.FacetTerm{Term: term, Count: count})
	}
	sort.Slice(terms, func(i, j int) bool {
		if terms[i].Count != terms[j].Count {
			return terms[i].Count > terms[j].Count
		}

		return terms[i].Term < terms[j].Term
	})
	if len(terms) > facetsFromResultsCap {
		terms = terms[:facetsFromResultsCap]
	}

	return searchcore.FacetGroup{Name: name, Terms: terms}, true
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

func portalFacets(
	query string,
	groups []searchcore.FacetGroup,
	scope string,
) []publicportal.FacetGroup {
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
		out = append(out, publicportal.FacetGroup{
			Title: facetTitles[group.Name], Scope: scope, Items: items,
		})
	}

	return out
}
