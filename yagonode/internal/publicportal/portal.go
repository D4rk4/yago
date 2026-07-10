// Package publicportal serves the anonymous public search portal: a minimal,
// server-rendered search page on the node's public HTTP listener that works
// without JavaScript and in legacy browsers. It is a surface distinct from the
// admin console (ADR-0020) and is mounted only when the operator enables it.
package publicportal

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

//go:embed templates/portal.tmpl
var templateFS embed.FS

const (
	brand    = "yago"
	htmlType = "text/html; charset=utf-8"
	// portalPageSize is how many results one portal page shows; portalMaxPage
	// bounds how deep a visitor can page so a crafted ?p= cannot request an
	// unbounded window.
	portalPageSize = 10
	portalMaxPage  = 50
)

// SearchResult is one rendered hit. Where a hit came from is shown through the
// Provenance badge (ADR-0019, ADR-0020).
type SearchResult struct {
	Title      string
	URL        string
	DisplayURL string
	Snippet    string
	// SnippetHTML carries the query-term-highlighted snippet (escaped text
	// plus <mark> only); when set it renders instead of the plain snippet.
	SnippetHTML template.HTML
	Host        string
	Date        string
	SizeName    string
	// CachedURL links this node's stored copy of the page; empty hides the link.
	CachedURL string
	// Provenance labels where the hit came from: "local" (this node's index),
	// "peer" (another swarm peer), or "web" (the external fallback).
	Provenance string
	// FaviconURL points at this node's favicon proxy for the result host, so
	// origin sites never see the searcher's browser before a click.
	FaviconURL string
	// Images carries the page's extracted images (proxied) for the image grid.
	Images []ResultImage
}

// ResultImage is one image-grid cell: the proxied thumbnail and its source page.
type ResultImage struct {
	ProxyURL string
	Alt      string
	PageURL  string
}

// SearchResults is the rendered outcome of a portal query. The per-source
// counts feed the transparency line so a searcher sees how much of the page
// came from this node, from peers, and from the web fallback.
type SearchResults struct {
	Query        string
	TotalResults int
	LocalCount   int
	PeerCount    int
	WebCount     int
	// PeersFailed counts peers that errored or timed out during the fan-out,
	// so «0 from peers» is distinguishable from «peers had nothing».
	PeersFailed int
	Results     []SearchResult
	// Recovered marks results found by the zero-result fuzzy retry, so the page
	// says these are close matches rather than exact ones.
	Recovered bool
	// DidYouMean and DidYouMeanURL offer the assembled spelling suggestion.
	DidYouMean    string
	DidYouMeanURL string
	// Facets renders the sidebar filter groups; empty hides the sidebar.
	Facets []FacetGroup
}

// FacetGroup is one sidebar filter dimension over the local matches.
type FacetGroup struct {
	Title string
	Items []FacetItem
}

// FacetItem is one clickable facet value; an empty URL renders a plain count.
type FacetItem struct {
	Label string
	Count int
	URL   string
}

// SearchSource runs a portal query against the node search core, returning the
// window of results starting at offset and holding at most limit hits. The dom
// parameter selects the content vertical ("", "image", "audio", "video", "app").
type SearchSource interface {
	Search(ctx context.Context, query, dom string, offset, limit int) (SearchResults, error)
}

// pagination carries the prev/next navigation for a results page. The URLs are
// built server-side (properly query-encoded) so the template never has to
// assemble a URL from parts.
type pagination struct {
	Page    int
	HasPrev bool
	HasNext bool
	PrevURL string
	NextURL string
	// Pages lists up to ten numbered page links around the current page.
	Pages []pageLink
}

type pageLink struct {
	Number  int
	URL     string
	Current bool
}

// pagerWindow is how many numbered page links the pager shows at most.
const pagerWindow = 10

type portalData struct {
	Brand      string
	Query      string
	Dom        string
	Verticals  []verticalTab
	Submitted  bool
	Error      string
	Results    SearchResults
	Pagination pagination
	NewTab     bool
	// RSSURL and JSONURL expose the current query in the machine-readable
	// output formats this node already serves; empty before a query is run.
	RSSURL  string
	JSONURL string
	// Elapsed is the human-readable search duration ("0.42 s") shown next to
	// the result count, so a searcher sees how fast the query ran.
	Elapsed string
	// ShownFrom and ShownTo are the 1-based rank range of the results rendered on
	// this page, so the meta line can distinguish the page window ("showing
	// 1–10") from the grand total match count, which spans this node and every
	// reachable peer. Set only when the page holds results.
	ShownFrom int
	ShownTo   int
}

// verticalTab is one content-vertical link above the results.
type verticalTab struct {
	Label   string
	URL     string
	Current bool
}

// portalVerticals builds the vertical tabs for the query.
func portalVerticals(query, dom string) []verticalTab {
	tabs := make([]verticalTab, 0, 5)
	for _, entry := range []struct{ label, value string }{
		{"All", ""},
		{"Images", "image"},
		{"Audio", "audio"},
		{"Video", "video"},
		{"Apps", "app"},
	} {
		values := url.Values{}
		values.Set("q", query)
		if entry.value != "" {
			values.Set("dom", entry.value)
		}
		tabs = append(tabs, verticalTab{
			Label:   entry.label,
			URL:     "/?" + values.Encode(),
			Current: dom == entry.value,
		})
	}

	return tabs
}

// portalClock feeds the query-duration display; tests substitute a scripted
// clock for a deterministic elapsed value.
var portalClock = time.Now

// Portal is the public search portal handler, mounted at the public root.
type Portal struct {
	page   *template.Template
	source SearchSource
	newTab bool
	// theme optionally renders operator-authored page templates (ADR-0033);
	// nil or a declining theme falls through to the built-in template.
	theme ThemeRenderer
}

// New builds the portal with its embedded template and search source.
// New builds the portal; newTab controls whether result links open in a new
// tab (with an accessible indicator) instead of the same-tab default.
func New(source SearchSource, newTab bool) *Portal {
	return &Portal{
		page:   template.Must(template.ParseFS(templateFS, "templates/portal.tmpl")),
		source: source,
		newTab: newTab,
	}
}

// ServeHTTP renders the search homepage and, when a query is present, its results.
func (p *Portal) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	dom := portalDom(r.URL.Query().Get("dom"))
	page := parsePortalPage(r.URL.Query().Get("p"))
	data := portalData{Brand: portalBrand(), Query: query, Dom: dom, NewTab: p.newTab}

	if query != "" {
		data.RSSURL = formatURL("/yacysearch.rss", query)
		data.JSONURL = formatURL("/yacysearch.json", query)
		offset := (page - 1) * portalPageSize
		data.Verticals = portalVerticals(query, dom)
		started := portalClock()
		results, err := p.source.Search(r.Context(), query, dom, offset, portalPageSize)
		if err != nil {
			slog.WarnContext(r.Context(), "public portal search failed", slog.Any("error", err))
			data.Error = "Search is temporarily unavailable."
		} else {
			data.Submitted = true
			data.Results = results
			if shown := len(results.Results); shown > 0 {
				data.ShownFrom = offset + 1
				data.ShownTo = offset + shown
			}
			data.Elapsed = elapsedSeconds(portalClock().Sub(started))
			data.Pagination = newPagination(
				query,
				page,
				offset,
				len(results.Results),
				results.TotalResults,
			)
		}
	}

	w.Header().Set("Content-Type", htmlType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")

	if p.theme != nil {
		if themed, ok := p.theme.Render(r.Context(), data.themePage(), data.themeView()); ok {
			// The themed page is rendered by the theme's Handlebars engine, which
			// auto-escapes every {{...}} interpolation of the view model; raw
			// {{{...}}} output is authored only by the authenticated operator
			// (ADR-0033), so writing the rendered document verbatim is the design.
			// nosemgrep: go.lang.security.audit.xss.no-io-writestring-to-responsewriter.no-io-writestring-to-responsewriter
			if _, err := io.WriteString(w, themed); err != nil {
				slog.WarnContext(r.Context(), "public portal render failed", slog.Any("error", err))
			}

			return
		}
	}

	if err := p.page.ExecuteTemplate(w, "portal", data); err != nil {
		slog.WarnContext(r.Context(), "public portal render failed", slog.Any("error", err))
	}
}

// parsePortalPage reads the 1-based page number from the ?p= parameter, clamping
// junk and out-of-range values into [1, portalMaxPage].
func parsePortalPage(raw string) int {
	page, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || page < 1 {
		return 1
	}
	if page > portalMaxPage {
		return portalMaxPage
	}

	return page
}

// newPagination decides which navigation links to show for the current page. A
// next link appears only while there are more results than the current window
// covers and the page cap has not been reached; a previous link appears past the
// first page.
func newPagination(query string, page, offset, shown, total int) pagination {
	nav := pagination{
		Page:    page,
		HasPrev: page > 1,
		HasNext: offset+shown < total && page < portalMaxPage,
	}
	if nav.HasPrev {
		nav.PrevURL = portalPageURL(query, page-1)
	}
	if nav.HasNext {
		nav.NextURL = portalPageURL(query, page+1)
	}
	nav.Pages = numberedPages(query, page, total)

	return nav
}

// numberedPages builds up to pagerWindow page links centered on the current
// page, bounded by the honest total and the portal's page cap.
func numberedPages(query string, page, total int) []pageLink {
	last := (total + portalPageSize - 1) / portalPageSize
	if last > portalMaxPage {
		last = portalMaxPage
	}
	if last <= 1 {
		return nil
	}
	start := page - pagerWindow/2
	if start+pagerWindow-1 > last {
		start = last - pagerWindow + 1
	}
	if start < 1 {
		start = 1
	}
	pages := make([]pageLink, 0, pagerWindow)
	for number := start; number <= last && len(pages) < pagerWindow; number++ {
		pages = append(pages, pageLink{
			Number:  number,
			URL:     portalPageURL(query, number),
			Current: number == page,
		})
	}

	return pages
}

func portalPageURL(query string, page int) string {
	values := url.Values{}
	values.Set("q", query)
	values.Set("p", strconv.Itoa(page))

	return "/?" + values.Encode()
}

// portalDom validates the requested content vertical.
func portalDom(raw string) string {
	switch raw {
	case "image", "audio", "video", "app":
		return raw
	default:
		return ""
	}
}

// formatURL links a machine-readable output format for the current query; the
// format endpoints live on the same public listener as the portal.
func formatURL(path string, query string) string {
	values := url.Values{}
	values.Set("query", query)

	return path + "?" + values.Encode()
}

// elapsedSeconds renders a search duration for the results meta line.
func elapsedSeconds(elapsed time.Duration) string {
	return fmt.Sprintf("%.2f s", elapsed.Seconds())
}
