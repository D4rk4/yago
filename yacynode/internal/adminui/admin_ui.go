// Package adminui serves the yago operator console as a server-rendered surface
// styled with IBM Carbon design tokens and progressively enhanced with htmx.
package adminui

import (
	"context"
	"embed"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

//go:embed assets/carbon.css assets/htmx.min.js
var assetFS embed.FS

// BasePath is where the console mounts on the operations listener.
const BasePath = "/admin/"

const (
	appName     = "yago"
	htmlType    = "text/html; charset=utf-8"
	contentPol  = "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	assetMaxAge = "public, max-age=86400"

	overviewPath        = "/admin/overview"
	overviewMetricsPath = "/admin/overview/metrics"
	searchPath          = "/admin/search"

	overviewUnavailable = "Node status is not available."
	searchUnavailable   = "Search is not available."
)

// NavItem is one entry in the console side navigation.
type NavItem struct {
	Title string
	Path  string
}

var navItems = []NavItem{
	{Title: "Overview", Path: overviewPath},
	{Title: "Search", Path: searchPath},
	{Title: "Crawler", Path: "/admin/crawl"},
	{Title: "Network", Path: "/admin/network"},
	{Title: "Index", Path: "/admin/index"},
	{Title: "Performance", Path: "/admin/performance"},
	{Title: "Configuration", Path: "/admin/configuration"},
	{Title: "Security", Path: "/admin/security"},
	{Title: "Logs", Path: "/admin/logs"},
}

// Options configures the console's data providers. A nil provider makes its
// section render a controlled unavailable state.
type Options struct {
	Overview OverviewSource
	Search   SearchSource
}

type sectionView struct {
	Heading   string
	Available bool
	Message   string
	Body      template.HTML
}

type pageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	Section    sectionView
	Overview   Overview
}

type searchPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	Section    sectionView
	Query      string
	Global     bool
	Submitted  bool
	Error      string
	Results    SearchResults
}

type templates struct {
	placeholder *template.Template
	overview    *template.Template
	search      *template.Template
}

// Console is the server-rendered admin console handler.
type Console struct {
	mux      *http.ServeMux
	tpl      templates
	sections map[string]sectionView
	overview OverviewSource
	search   SearchSource
}

// New builds the console with its embedded templates, assets, and providers.
func New(opts Options) *Console {
	assets, err := fs.Sub(assetFS, "assets")
	if err != nil {
		panic(err)
	}

	console := &Console{
		mux:      http.NewServeMux(),
		tpl:      buildTemplates(),
		sections: defaultSections(),
		overview: opts.Overview,
		search:   opts.Search,
	}
	console.registerRoutes(assets)

	return console
}

func buildTemplates() templates {
	layout := template.Must(template.ParseFS(templateFS, "templates/layout.tmpl"))
	clone := func(fns template.FuncMap, files ...string) *template.Template {
		return template.Must(template.Must(layout.Clone()).Funcs(fns).ParseFS(templateFS, files...))
	}

	return templates{
		placeholder: clone(nil, "templates/placeholder.tmpl"),
		overview:    clone(overviewFuncs, "templates/overview.tmpl", "templates/metrics.tmpl"),
		search:      clone(nil, "templates/search.tmpl"),
	}
}

func (c *Console) registerRoutes(assets fs.FS) {
	c.mux.Handle("GET /admin/assets/", assetHandler(assets))
	c.mux.HandleFunc("GET /admin/{$}", handleIndex)
	c.mux.HandleFunc("GET "+overviewPath, c.handleOverview)
	c.mux.HandleFunc("GET "+overviewMetricsPath, c.handleOverviewMetrics)
	c.mux.HandleFunc("GET "+searchPath, c.handleSearch)

	for _, item := range navItems {
		if dynamicSection(item.Path) {
			continue
		}
		c.mux.HandleFunc("GET "+item.Path, c.sectionHandler(item.Path))
	}
}

func dynamicSection(path string) bool {
	return path == overviewPath || path == searchPath
}

// ServeHTTP dispatches to the console's internal router.
func (c *Console) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mux.ServeHTTP(w, r)
}

func (c *Console) sectionHandler(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		view, ok := c.sections[path]
		if !ok {
			http.NotFound(w, r)

			return
		}

		c.render(r.Context(), w, c.tpl.placeholder, "layout", pageData{
			AppName: appName, ActivePath: path, Nav: navItems, Section: view,
		})
	}
}

func (c *Console) handleOverview(w http.ResponseWriter, r *http.Request) {
	if c.overview == nil {
		c.renderUnavailable(w, r, overviewPath, "Overview", overviewUnavailable)

		return
	}

	c.render(r.Context(), w, c.tpl.overview, "layout", pageData{
		AppName: appName, ActivePath: overviewPath, Nav: navItems,
		Section:  sectionView{Heading: "Overview", Available: true},
		Overview: c.overview.Overview(r.Context()),
	})
}

func (c *Console) handleOverviewMetrics(w http.ResponseWriter, r *http.Request) {
	if c.overview == nil {
		http.NotFound(w, r)

		return
	}

	c.render(r.Context(), w, c.tpl.overview, "overview-metrics", c.overview.Overview(r.Context()))
}

func (c *Console) handleSearch(w http.ResponseWriter, r *http.Request) {
	if c.search == nil {
		c.renderUnavailable(w, r, searchPath, "Search", searchUnavailable)

		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	data := searchPageData{
		AppName: appName, ActivePath: searchPath, Nav: navItems,
		Section: sectionView{Heading: "Search", Available: true},
		Query:   query, Global: r.URL.Query().Get("scope") == "global",
	}

	if query != "" {
		data.Submitted = true
		results, err := c.search.Search(r.Context(), SearchQuery{Query: query, Global: data.Global})
		if err != nil {
			slog.WarnContext(r.Context(), "admin search failed", slog.Any("error", err))
			data.Error = "Search failed."
		} else {
			data.Results = results
		}
	}

	c.render(r.Context(), w, c.tpl.search, "layout", data)
}

func (c *Console) renderUnavailable(
	w http.ResponseWriter,
	r *http.Request,
	path, heading, message string,
) {
	c.render(r.Context(), w, c.tpl.placeholder, "layout", pageData{
		AppName: appName, ActivePath: path, Nav: navItems,
		Section: sectionView{Heading: heading, Message: message},
	})
}

func (c *Console) render(
	ctx context.Context,
	w http.ResponseWriter,
	tpl *template.Template,
	name string,
	data any,
) {
	writeHTMLHeaders(w)
	if err := tpl.ExecuteTemplate(w, name, data); err != nil {
		slog.WarnContext(ctx, "admin console render failed",
			slog.String("template", name), slog.Any("error", err))
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, overviewPath, http.StatusFound)
}

func writeHTMLHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Content-Type", htmlType)
	header.Set("Content-Security-Policy", contentPol)
	header.Set("X-Content-Type-Options", "nosniff")
}

func assetHandler(assets fs.FS) http.Handler {
	fileServer := http.StripPrefix("/admin/assets/", http.FileServerFS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", assetMaxAge)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fileServer.ServeHTTP(w, r)
	})
}
