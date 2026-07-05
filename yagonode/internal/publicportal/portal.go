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

// SearchResult is one rendered hit. Marked is set for DDGS web-fallback hits so
// the portal shows the visible [ddgs] marker (ADR-0019, ADR-0020).
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
	Marked      bool
	// CachedURL links this node's stored copy of the page; empty hides the link.
	CachedURL string
}

// SearchResults is the rendered outcome of a portal query.
type SearchResults struct {
	Query        string
	TotalResults int
	Results      []SearchResult
}

// SearchSource runs a portal query against the node search core, returning the
// window of results starting at offset and holding at most limit hits.
type SearchSource interface {
	Search(ctx context.Context, query string, offset, limit int) (SearchResults, error)
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
}

type portalData struct {
	Brand      string
	Query      string
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
}

// portalClock feeds the query-duration display; tests substitute a scripted
// clock for a deterministic elapsed value.
var portalClock = time.Now

// Portal is the public search portal handler, mounted at the public root.
type Portal struct {
	page   *template.Template
	source SearchSource
	newTab bool
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
	page := parsePortalPage(r.URL.Query().Get("p"))
	data := portalData{Brand: brand, Query: query, NewTab: p.newTab}

	if query != "" {
		data.RSSURL = formatURL("/yacysearch.rss", query)
		data.JSONURL = formatURL("/yacysearch.json", query)
		offset := (page - 1) * portalPageSize
		started := portalClock()
		results, err := p.source.Search(r.Context(), query, offset, portalPageSize)
		if err != nil {
			slog.WarnContext(r.Context(), "public portal search failed", slog.Any("error", err))
			data.Error = "Search is temporarily unavailable."
		} else {
			data.Submitted = true
			data.Results = results
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

	return nav
}

func portalPageURL(query string, page int) string {
	values := url.Values{}
	values.Set("q", query)
	values.Set("p", strconv.Itoa(page))

	return "/?" + values.Encode()
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
