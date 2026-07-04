// Package publicportal serves the anonymous public search portal: a minimal,
// server-rendered search page on the node's public HTTP listener that works
// without JavaScript and in legacy browsers. It is a surface distinct from the
// admin console (ADR-0020) and is mounted only when the operator enables it.
package publicportal

import (
	"context"
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
)

//go:embed templates/portal.tmpl
var templateFS embed.FS

const (
	brand    = "yago"
	htmlType = "text/html; charset=utf-8"
)

// SearchResult is one rendered hit. Marked is set for DDGS web-fallback hits so
// the portal shows the visible [ddgs] marker (ADR-0019, ADR-0020).
type SearchResult struct {
	Title      string
	URL        string
	DisplayURL string
	Snippet    string
	Marked     bool
}

// SearchResults is the rendered outcome of a portal query.
type SearchResults struct {
	Query        string
	TotalResults int
	Results      []SearchResult
}

// SearchSource runs a portal query against the node search core.
type SearchSource interface {
	Search(ctx context.Context, query string) (SearchResults, error)
}

type portalData struct {
	Brand     string
	Query     string
	Submitted bool
	Error     string
	Results   SearchResults
}

// Portal is the public search portal handler, mounted at the public root.
type Portal struct {
	page   *template.Template
	source SearchSource
}

// New builds the portal with its embedded template and search source.
func New(source SearchSource) *Portal {
	return &Portal{
		page:   template.Must(template.ParseFS(templateFS, "templates/portal.tmpl")),
		source: source,
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
	data := portalData{Brand: brand, Query: query}

	if query != "" {
		results, err := p.source.Search(r.Context(), query)
		if err != nil {
			slog.WarnContext(r.Context(), "public portal search failed", slog.Any("error", err))
			data.Error = "Search is temporarily unavailable."
		} else {
			data.Submitted = true
			data.Results = results
		}
	}

	w.Header().Set("Content-Type", htmlType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")

	if err := p.page.ExecuteTemplate(w, "portal", data); err != nil {
		slog.WarnContext(r.Context(), "public portal render failed", slog.Any("error", err))
	}
}
