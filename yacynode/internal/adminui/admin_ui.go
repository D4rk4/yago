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
	overviewUnavailable = "Node status is not available."
)

// NavItem is one entry in the console side navigation.
type NavItem struct {
	Title string
	Path  string
}

var navItems = []NavItem{
	{Title: "Overview", Path: overviewPath},
	{Title: "Search", Path: "/admin/search"},
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

// Console is the server-rendered admin console handler.
type Console struct {
	mux         *http.ServeMux
	placeholder *template.Template
	overviewTpl *template.Template
	sections    map[string]sectionView
	overview    OverviewSource
}

// New builds the console with its embedded templates, assets, and providers.
func New(opts Options) *Console {
	assets, err := fs.Sub(assetFS, "assets")
	if err != nil {
		panic(err)
	}

	placeholder, overviewTpl := buildTemplates()
	console := &Console{
		mux:         http.NewServeMux(),
		placeholder: placeholder,
		overviewTpl: overviewTpl,
		sections:    defaultSections(),
		overview:    opts.Overview,
	}
	console.registerRoutes(assets)

	return console
}

func buildTemplates() (placeholder, overview *template.Template) {
	layout := template.Must(template.ParseFS(templateFS, "templates/layout.tmpl"))
	placeholder = template.Must(
		template.Must(layout.Clone()).ParseFS(templateFS, "templates/placeholder.tmpl"),
	)
	overview = template.Must(
		template.Must(layout.Clone()).Funcs(overviewFuncs).
			ParseFS(templateFS, "templates/overview.tmpl", "templates/metrics.tmpl"),
	)

	return placeholder, overview
}

func (c *Console) registerRoutes(assets fs.FS) {
	c.mux.Handle("GET /admin/assets/", assetHandler(assets))
	c.mux.HandleFunc("GET /admin/{$}", handleIndex)
	c.mux.HandleFunc("GET "+overviewPath, c.handleOverview)
	c.mux.HandleFunc("GET "+overviewMetricsPath, c.handleOverviewMetrics)

	for _, item := range navItems {
		if item.Path == overviewPath {
			continue
		}
		c.mux.HandleFunc("GET "+item.Path, c.sectionHandler(item.Path))
	}
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

		c.render(r.Context(), w, c.placeholder, "layout", pageData{
			AppName: appName, ActivePath: path, Nav: navItems, Section: view,
		})
	}
}

func (c *Console) handleOverview(w http.ResponseWriter, r *http.Request) {
	if c.overview == nil {
		c.render(r.Context(), w, c.placeholder, "layout", pageData{
			AppName: appName, ActivePath: overviewPath, Nav: navItems,
			Section: sectionView{Heading: "Overview", Message: overviewUnavailable},
		})

		return
	}

	c.render(r.Context(), w, c.overviewTpl, "layout", pageData{
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

	c.render(r.Context(), w, c.overviewTpl, "overview-metrics", c.overview.Overview(r.Context()))
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
