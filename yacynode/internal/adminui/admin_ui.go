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
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yacynode/internal/adminauth"
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
	crawlPath           = "/admin/crawl"
	configPath          = "/admin/configuration"
	indexPath           = "/admin/index"
	networkPath         = "/admin/network"
	logsPath            = "/admin/logs"
	logsEventsPath      = "/admin/logs/events"

	overviewUnavailable = "Node status is not available."
	searchUnavailable   = "Search is not available."
	crawlUnavailable    = "The crawler is not available on this node."
	configUnavailable   = "Configuration is not available."
	indexUnavailable    = "The search index is not available."
	networkUnavailable  = "Network status is not available."
	logsUnavailable     = "Event log is not available."
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
	{Title: "Index", Path: indexPath},
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
	Crawl    CrawlSource
	Index    IndexSource
	Network  NetworkSource
	Config   ConfigSource
	Logs     LogsSource
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
	CSRF       string
	Section    sectionView
	Overview   Overview
	Index      IndexStats
	Network    NetworkStatus
	Config     ConfigView
	Logs       []LogEntry
}

type searchPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Query      string
	Global     bool
	Submitted  bool
	Error      string
	Results    SearchResults
}

type crawlForm struct {
	Name     string
	Seeds    string
	Mode     string
	Scope    string
	MaxDepth int
}

type crawlPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Form       crawlForm
	Result     *CrawlDispatch
	Error      string
}

func csrfToken(r *http.Request) string {
	token, _ := adminauth.CSRFTokenFromContext(r.Context())

	return token
}

type templates struct {
	placeholder *template.Template
	overview    *template.Template
	search      *template.Template
	crawl       *template.Template
	index       *template.Template
	network     *template.Template
	config      *template.Template
	logs        *template.Template
}

// Console is the server-rendered admin console handler.
type Console struct {
	mux      *http.ServeMux
	tpl      templates
	sections map[string]sectionView
	overview OverviewSource
	search   SearchSource
	crawl    CrawlSource
	index    IndexSource
	network  NetworkSource
	config   ConfigSource
	logs     LogsSource
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
		crawl:    opts.Crawl,
		index:    opts.Index,
		network:  opts.Network,
		config:   opts.Config,
		logs:     opts.Logs,
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
		crawl:       clone(nil, "templates/crawl.tmpl"),
		index:       clone(nil, "templates/index.tmpl"),
		network:     clone(nil, "templates/network.tmpl"),
		config:      clone(nil, "templates/config.tmpl"),
		logs:        clone(nil, "templates/logs.tmpl", "templates/logs_table.tmpl"),
	}
}

func (c *Console) registerRoutes(assets fs.FS) {
	c.mux.Handle("GET /admin/assets/", assetHandler(assets))
	c.mux.HandleFunc("GET /admin/{$}", handleRoot)
	c.mux.HandleFunc("GET "+overviewPath, c.handleOverview)
	c.mux.HandleFunc("GET "+overviewMetricsPath, c.handleOverviewMetrics)
	c.mux.HandleFunc("GET "+searchPath, c.handleSearch)
	c.mux.HandleFunc("GET "+crawlPath, c.handleCrawl)
	c.mux.HandleFunc("POST "+crawlPath, c.handleCrawlStart)
	c.mux.HandleFunc("GET "+indexPath, c.handleIndex)
	c.mux.HandleFunc("GET "+networkPath, c.handleNetwork)
	c.mux.HandleFunc("GET "+configPath, c.handleConfig)
	c.mux.HandleFunc("GET "+logsPath, c.handleLogs)
	c.mux.HandleFunc("GET "+logsEventsPath, c.handleLogsEvents)

	for _, item := range navItems {
		if dynamicSection(item.Path) {
			continue
		}
		c.mux.HandleFunc("GET "+item.Path, c.sectionHandler(item.Path))
	}
}

func dynamicSection(path string) bool {
	return path == overviewPath || path == searchPath || path == crawlPath ||
		path == indexPath || path == networkPath || path == configPath ||
		path == logsPath
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
			AppName: appName, ActivePath: path, Nav: navItems,
			CSRF: csrfToken(r), Section: view,
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
		CSRF:     csrfToken(r),
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

func (c *Console) handleIndex(w http.ResponseWriter, r *http.Request) {
	if c.index == nil {
		c.renderUnavailable(w, r, indexPath, "Index", indexUnavailable)

		return
	}

	c.render(r.Context(), w, c.tpl.index, "layout", pageData{
		AppName: appName, ActivePath: indexPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Index", Available: true},
		Index:   c.index.Index(r.Context()),
	})
}

func (c *Console) handleLogs(w http.ResponseWriter, r *http.Request) {
	if c.logs == nil {
		c.renderUnavailable(w, r, logsPath, "Logs", logsUnavailable)

		return
	}

	c.render(r.Context(), w, c.tpl.logs, "layout", pageData{
		AppName: appName, ActivePath: logsPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Logs", Available: true},
		Logs:    c.logs.Logs(r.Context()),
	})
}

func (c *Console) handleLogsEvents(w http.ResponseWriter, r *http.Request) {
	if c.logs == nil {
		http.NotFound(w, r)

		return
	}

	c.render(r.Context(), w, c.tpl.logs, "logs-table", c.logs.Logs(r.Context()))
}

func (c *Console) handleNetwork(w http.ResponseWriter, r *http.Request) {
	if c.network == nil {
		c.renderUnavailable(w, r, networkPath, "Network", networkUnavailable)

		return
	}

	c.render(r.Context(), w, c.tpl.network, "layout", pageData{
		AppName: appName, ActivePath: networkPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Network", Available: true},
		Network: c.network.Network(r.Context()),
	})
}

func (c *Console) handleConfig(w http.ResponseWriter, r *http.Request) {
	if c.config == nil {
		c.renderUnavailable(w, r, configPath, "Configuration", configUnavailable)

		return
	}

	c.render(r.Context(), w, c.tpl.config, "layout", pageData{
		AppName: appName, ActivePath: configPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Configuration", Available: true},
		Config:  c.config.Config(r.Context()),
	})
}

func (c *Console) handleSearch(w http.ResponseWriter, r *http.Request) {
	if c.search == nil {
		c.renderUnavailable(w, r, searchPath, "Search", searchUnavailable)

		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	data := searchPageData{
		AppName: appName, ActivePath: searchPath, Nav: navItems,
		CSRF:    csrfToken(r),
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

func (c *Console) handleCrawl(w http.ResponseWriter, r *http.Request) {
	if c.crawl == nil {
		c.renderUnavailable(w, r, crawlPath, "Crawler", crawlUnavailable)

		return
	}

	c.render(r.Context(), w, c.tpl.crawl, "layout", c.crawlPage(r, defaultCrawlForm()))
}

func (c *Console) handleCrawlStart(w http.ResponseWriter, r *http.Request) {
	if c.crawl == nil {
		c.renderUnavailable(w, r, crawlPath, "Crawler", crawlUnavailable)

		return
	}

	form := parseCrawlForm(r)
	data := c.crawlPage(r, form)

	seeds := splitSeeds(form.Seeds)
	if len(seeds) == 0 {
		data.Error = "Enter at least one seed URL."
		c.render(r.Context(), w, c.tpl.crawl, "layout", data)

		return
	}

	result, err := c.crawl.Start(r.Context(), CrawlStart{
		Name:     form.Name,
		Seeds:    seeds,
		Mode:     form.Mode,
		Scope:    form.Scope,
		MaxDepth: form.MaxDepth,
	})
	if err != nil {
		slog.WarnContext(r.Context(), "admin crawl start failed", slog.Any("error", err))
		data.Error = "Crawl start failed. Check the seed URLs and options, then try again."
	} else {
		data.Result = &result
	}

	c.render(r.Context(), w, c.tpl.crawl, "layout", data)
}

func (c *Console) crawlPage(r *http.Request, form crawlForm) crawlPageData {
	return crawlPageData{
		AppName: appName, ActivePath: crawlPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Crawler", Available: true},
		Form:    form,
	}
}

func defaultCrawlForm() crawlForm {
	return crawlForm{Mode: "url", Scope: "domain", MaxDepth: 3}
}

func parseCrawlForm(r *http.Request) crawlForm {
	depth, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("maxDepth")))

	return crawlForm{
		Name:     strings.TrimSpace(r.PostFormValue("name")),
		Seeds:    r.PostFormValue("seeds"),
		Mode:     r.PostFormValue("mode"),
		Scope:    r.PostFormValue("scope"),
		MaxDepth: depth,
	}
}

func splitSeeds(raw string) []string {
	var seeds []string
	for _, line := range strings.Split(raw, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			seeds = append(seeds, trimmed)
		}
	}

	return seeds
}

func (c *Console) renderUnavailable(
	w http.ResponseWriter,
	r *http.Request,
	path, heading, message string,
) {
	c.render(r.Context(), w, c.tpl.placeholder, "layout", pageData{
		AppName: appName, ActivePath: path, Nav: navItems,
		CSRF:    csrfToken(r),
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

func handleRoot(w http.ResponseWriter, r *http.Request) {
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
