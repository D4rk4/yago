package publicportal

import "context"

// ThemeRenderer renders an operator-authored portal page (ADR-0033). Render
// reports false when the theme is off, missing, or failing, and the portal
// then serves its built-in template — the fallback that keeps a broken
// operator theme from taking the public surface down.
type ThemeRenderer interface {
	Render(ctx context.Context, page string, view map[string]any) (string, bool)
}

// Theme page names shared with the theme store by value, so the portal does
// not depend on the store package.
const (
	themePageSearch  = "search"
	themePageResults = "results"
)

// SetTheme installs the operator theme renderer; the portal keeps serving its
// built-in template when none is installed.
func (p *Portal) SetTheme(theme ThemeRenderer) {
	p.theme = theme
}

// themePage picks which operator template a request renders: the search page
// before a query, the results page once one was submitted (errors included, so
// the operator styles the failure state too).
func (d portalData) themePage() string {
	if d.Query == "" {
		return themePageSearch
	}

	return themePageResults
}

// themeView flattens the portal data into the documented, stable view model an
// operator template interpolates. Keys are lowerCamel; crawled-content fields
// stay plain strings that Handlebars {{...}} expressions escape, while
// snippetHtml carries the pre-escaped highlighted snippet for {{{...}}} use.
func (d portalData) themeView() map[string]any {
	results := make([]map[string]any, 0, len(d.Results.Results))
	for _, hit := range d.Results.Results {
		results = append(results, hitView(hit))
	}

	return map[string]any{
		"brand":         d.Brand,
		"query":         d.Query,
		"dom":           d.Dom,
		"imageVertical": d.Dom == "image",
		"submitted":     d.Submitted,
		"error":         d.Error,
		"newTab":        d.NewTab,
		"rssUrl":        d.RSSURL,
		"jsonUrl":       d.JSONURL,
		"elapsed":       d.Elapsed,
		"verticals":     verticalViews(d.Verticals),
		"results": map[string]any{
			"query":         d.Results.Query,
			"totalResults":  d.Results.TotalResults,
			"localCount":    d.Results.LocalCount,
			"peerCount":     d.Results.PeerCount,
			"webCount":      d.Results.WebCount,
			"peersFailed":   d.Results.PeersFailed,
			"recovered":     d.Results.Recovered,
			"didYouMean":    d.Results.DidYouMean,
			"didYouMeanUrl": d.Results.DidYouMeanURL,
			"results":       results,
			"facets":        facetViews(d.Results.Facets),
		},
		"pagination": map[string]any{
			"show": d.Pagination.HasPrev || d.Pagination.HasNext ||
				len(d.Pagination.Pages) > 0,
			"page":    d.Pagination.Page,
			"hasPrev": d.Pagination.HasPrev,
			"hasNext": d.Pagination.HasNext,
			"prevUrl": d.Pagination.PrevURL,
			"nextUrl": d.Pagination.NextURL,
			"pages":   pageViews(d.Pagination.Pages),
		},
	}
}

func hitView(hit SearchResult) map[string]any {
	images := make([]map[string]any, 0, len(hit.Images))
	for _, image := range hit.Images {
		images = append(images, map[string]any{
			"proxyUrl": image.ProxyURL,
			"alt":      image.Alt,
			"pageUrl":  image.PageURL,
		})
	}

	return map[string]any{
		"title":           hit.Title,
		"url":             hit.URL,
		"displayUrl":      hit.DisplayURL,
		"snippet":         hit.Snippet,
		"snippetHtml":     string(hit.SnippetHTML),
		"host":            hit.Host,
		"date":            hit.Date,
		"sizeName":        hit.SizeName,
		"cachedUrl":       hit.CachedURL,
		"provenance":      hit.Provenance,
		"provenanceLabel": provenanceLabel(hit.Provenance),
		"faviconUrl":      hit.FaviconURL,
		"images":          images,
	}
}

func provenanceLabel(provenance string) string {
	if provenance == "ddgs" {
		return "[ddgs]"
	}

	return provenance
}

func verticalViews(tabs []verticalTab) []map[string]any {
	views := make([]map[string]any, 0, len(tabs))
	for _, tab := range tabs {
		views = append(views, map[string]any{
			"label":   tab.Label,
			"url":     tab.URL,
			"current": tab.Current,
		})
	}

	return views
}

func facetViews(groups []FacetGroup) []map[string]any {
	views := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		items := make([]map[string]any, 0, len(group.Items))
		for _, item := range group.Items {
			items = append(items, map[string]any{
				"label": item.Label,
				"count": item.Count,
				"url":   item.URL,
			})
		}
		views = append(views, map[string]any{"title": group.Title, "items": items})
	}

	return views
}

func pageViews(pages []pageLink) []map[string]any {
	views := make([]map[string]any, 0, len(pages))
	for _, page := range pages {
		views = append(views, map[string]any{
			"number":  page.Number,
			"url":     page.URL,
			"current": page.Current,
		})
	}

	return views
}
