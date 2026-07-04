// Package adminui serves the yago operator console as a server-rendered surface
// styled with IBM Carbon design tokens and progressively enhanced with htmx.
package adminui

import (
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
	contentPol  = "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self' data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	assetMaxAge = "public, max-age=86400"
)

// NavItem is one entry in the console side navigation.
type NavItem struct {
	Title string
	Path  string
}

var navItems = []NavItem{
	{Title: "Overview", Path: "/admin/overview"},
	{Title: "Search", Path: "/admin/search"},
	{Title: "Crawler", Path: "/admin/crawl"},
	{Title: "Network", Path: "/admin/network"},
	{Title: "Index", Path: "/admin/index"},
	{Title: "Performance", Path: "/admin/performance"},
	{Title: "Configuration", Path: "/admin/configuration"},
	{Title: "Security", Path: "/admin/security"},
	{Title: "Logs", Path: "/admin/logs"},
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
}

// Console is the server-rendered admin console handler.
type Console struct {
	mux      *http.ServeMux
	page     *template.Template
	sections map[string]sectionView
}

// New builds the console with its embedded templates and assets.
func New() *Console {
	assets, err := fs.Sub(assetFS, "assets")
	if err != nil {
		panic(err)
	}

	console := &Console{
		page: template.Must(
			template.ParseFS(templateFS, "templates/layout.tmpl", "templates/placeholder.tmpl"),
		),
		sections: defaultSections(),
	}

	console.mux = http.NewServeMux()
	console.mux.Handle("GET /admin/assets/", assetHandler(assets))
	console.mux.HandleFunc("GET /admin/{$}", handleIndex)

	for _, item := range navItems {
		console.mux.HandleFunc("GET "+item.Path, console.sectionHandler(item.Path))
	}

	return console
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

		data := pageData{AppName: appName, ActivePath: path, Nav: navItems, Section: view}
		w.Header().Set("Content-Type", htmlType)
		w.Header().Set("Content-Security-Policy", contentPol)
		w.Header().Set("X-Content-Type-Options", "nosniff")

		if err := c.page.ExecuteTemplate(w, "layout", data); err != nil {
			slog.WarnContext(r.Context(), "admin console render failed",
				slog.String("path", path), slog.Any("error", err))
		}
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/overview", http.StatusFound)
}

func assetHandler(assets fs.FS) http.Handler {
	fileServer := http.StripPrefix("/admin/assets/", http.FileServerFS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", assetMaxAge)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fileServer.ServeHTTP(w, r)
	})
}
