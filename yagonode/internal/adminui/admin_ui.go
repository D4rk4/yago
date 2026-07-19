// Package adminui serves the yago operator console as a server-rendered surface
// styled with IBM Carbon design tokens and progressively enhanced with htmx.
package adminui

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminauth"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

//go:embed assets/carbon.css assets/photon.css assets/photon_shell.css assets/htmx.min.js assets/autocomplete.js assets/tabs.js assets/portal_designer.js assets/portal_designer.css assets/vendor assets/fonts assets/icons
var assetFS embed.FS

// BasePath is where the console mounts on the operations listener.
const BasePath = "/admin/"

const (
	appName          = "yago"
	htmlType         = "text/html; charset=utf-8"
	contentPol       = "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
	portalContentPol = "default-src 'none'; style-src 'self' 'unsafe-inline'; font-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

	overviewPath        = "/admin/overview"
	overviewMetricsPath = "/admin/overview/metrics"
	searchPath          = "/admin/search"
	activityPath        = "/admin/activity"
	crawlPath           = "/admin/crawl"
	crawlMonitorPath    = "/admin/crawl/monitor"
	crawlControlPath    = "/admin/crawl/control"
	crawlSchedulePath   = "/admin/crawl/schedule"
	crawlProfilePath    = "/admin/crawl/profile"
	crawlRunPath        = "/admin/crawl/run"
	configPath          = "/admin/configuration"
	indexPath           = "/admin/index"
	indexDocumentPath   = "/admin/index/document"
	indexDeletePath     = "/admin/index/delete"
	indexRebuildPath    = "/admin/index/rebuild"
	blacklistPath       = "/admin/index/blacklist"
	blacklistTestPath   = "/admin/index/blacklist/test"
	blacklistExportPath = "/admin/index/blacklist/export"
	blacklistImportPath = "/admin/index/blacklist/import"
	indexExportPath     = "/admin/index/export"
	networkPath         = "/admin/network"
	networkSelfTestPath = "/admin/network/public-endpoint/test"
	networkPeerPath     = "/admin/network/peer"
	peerBlockPath       = "/admin/network/peer/block"
	seedlistRefreshPath = "/admin/network/seedlist/refresh"
	logsPath            = "/admin/logs"
	logsEventsPath      = "/admin/logs/events"
	securityPath        = "/admin/security"
	performancePath     = "/admin/performance"
	yagorankPath        = "/admin/yagorank"

	// adminSearchPageSize is how many results one admin search page shows;
	// adminSearchMaxPage bounds how deep a crafted ?p= can page.
	adminSearchPageSize = 20
	adminSearchMaxPage  = 50

	overviewUnavailable    = "Node status is not available."
	searchUnavailable      = "Search is not available."
	crawlUnavailable       = "The crawler is not available on this node."
	configUnavailable      = "Configuration is not available."
	indexUnavailable       = "The search index is not available."
	networkUnavailable     = "Network status is not available."
	peerDetailUnavailable  = "Peer detail is not available."
	logsUnavailable        = "Event log is not available."
	securityUnavailable    = "Security settings are not available."
	performanceUnavailable = "Performance metrics are not available."
	activityUnavailable    = "Search activity is not recorded: query logging is off."
)

// NavItem is one entry in the console side navigation.
type NavItem struct {
	Title string
	Path  string
	Icon  string
}

var navItems = []NavItem{
	{Title: "Overview", Path: overviewPath, Icon: "icons/desktop.svg"},
	{Title: "Search", Path: searchPath, Icon: "icons/system-search.svg"},
	{Title: "Activity", Path: activityPath, Icon: "icons/appointment.svg"},
	{Title: "Public portal", Path: portalPath, Icon: "icons/applications-internet.svg"},
	{Title: "Crawler", Path: "/admin/crawl", Icon: "icons/gnome-robots.svg"},
	{Title: "Network", Path: "/admin/network", Icon: "icons/network-workgroup.svg"},
	{Title: "Index", Path: indexPath, Icon: "icons/drive-partition.svg"},
	{Title: "YagoRank", Path: yagorankPath, Icon: "icons/applications-science.svg"},
	{Title: "Performance", Path: "/admin/performance", Icon: "icons/speedometer.svg"},
	{Title: "Backup", Path: backupPath, Icon: "icons/media-floppy.svg"},
	{Title: "Configuration", Path: "/admin/configuration", Icon: "icons/preferences-system.svg"},
	{Title: "Security", Path: "/admin/security", Icon: "icons/security-high.svg"},
	{Title: "Logs", Path: "/admin/logs", Icon: "icons/accessories-text-editor.svg"},
	{Title: "Restart", Path: restartPath, Icon: "icons/view-refresh.svg"},
}

// Options configures the console's data providers. A nil provider makes its
// section render a controlled unavailable state.
type Options struct {
	Overview             OverviewSource
	Search               SearchSource
	SearchExplanation    SearchExplanationSource
	Activity             ActivitySource
	Crawl                CrawlSource
	CrawlFormats         CrawlFormatsSource
	Monitor              CrawlMonitorSource
	CrawlerFetchActivity CrawlerFetchActivitySource
	StoragePressure      StoragePressureStatusSource
	Schedules            CrawlScheduleSource
	SavedCrawlProfiles   SavedCrawlProfileSource
	CrawlRunDetails      CrawlRunDetailSource
	Control              CrawlControlSource
	Index                IndexSource
	Documents            DocumentBrowserSource
	DocumentDetail       DocumentDetailSource
	IndexAdmin           IndexAdminSource
	IndexRebuild         IndexRebuildScheduler
	Blacklist            BlacklistSource
	IndexExport          IndexExporter
	Compactor            Compactor
	Network              NetworkSource
	NetworkSelfTest      NetworkSelfTester
	Config               ConfigSource
	Settings             SettingsSource
	PublicSearch         PublicSearchStatusSource
	// Theme persists the operator portal design (ADR-0033); nil renders the
	// design tabs as placeholders.
	Theme    ThemeStore
	Binding  BindingSource
	Logs     LogsSource
	Security SecuritySource
	Terms    TermSource
	Schema   []SchemaGroup
	// Ranking backs the YagoRank section; nil renders it unavailable.
	Ranking     RankingSource
	Performance PerformanceSource
	// PerformanceHistory feeds the Performance page's sampled history charts.
	PerformanceHistory PerformanceHistorySource
	// Backup reports storage status for the Backup & restore page.
	Backup          BackupSource
	PeerDetail      PeerDetailSource
	PeerNews        PeerNewsSource
	SeedlistRefresh SeedlistRefreshSource
	// SearchLinksNewTab opens result links in a new tab with an accessible
	// indicator; the default keeps NN/G same-tab navigation.
	SearchLinksNewTab bool
	// Restart requests a graceful node restart; nil hides the action.
	Restart func()
	// RestartCrawlers signals every connected crawler to restart over the gRPC
	// control plane and returns how many were signalled; nil hides the action.
	RestartCrawlers func() int
	PeerBlock       PeerBlockSource
	// SearchSuggest, when set, serves OpenSearch suggestion JSON for the console
	// search box autocomplete at GET /admin/search/suggest.
	SearchSuggest http.Handler
	// PublicBaseURL is the operator-configured public origin of the search
	// portal (the OpenSearch external base URL); when set the header's Public
	// search link points straight at it.
	PublicBaseURL string
	// PublicAddr is the dedicated public listener's bind address (e.g. ":8080").
	// When PublicBaseURL is unset the Public search link is derived from the
	// request host and this port; an empty/disabled address hides the link.
	PublicAddr string
}

type sectionView struct {
	Heading   string
	Available bool
	Message   string
	Body      template.HTML
}

type pageData struct {
	AppName                string
	ActivePath             string
	Nav                    []NavItem
	CSRF                   string
	Section                sectionView
	Overview               Overview
	Index                  IndexStats
	Network                NetworkStatus
	NetworkSelfTestEnabled bool
	NetworkSelfTestNotice  string
	NetworkSelfTestError   string
	PeerTable              PeerTableView
	PeerLinks              bool
	PeerNews               []PeerNewsItem
	PeerNewsEnabled        bool
	PeerNewsAvailable      bool
	SeedlistRefresh        bool
	Config                 ConfigView
	Logs                   []LogEntry
}

type peerDetailPageData struct {
	AppName      string
	ActivePath   string
	Nav          []NavItem
	CSRF         string
	Section      sectionView
	Peer         PeerDetail
	BlockEnabled bool
}

type searchPageData struct {
	AppName        string
	ActivePath     string
	Nav            []NavItem
	CSRF           string
	Section        sectionView
	Query          string
	Global         bool
	Filters        SearchFilters
	ContentDomains []searchContentDomainOption
	Submitted      bool
	Error          string
	Results        SearchResults
	Pagination     SearchPagination
	NewTab         bool
	SuggestEnabled bool
	ExplainURL     string
}

type crawlForm struct {
	Name                     string
	Seeds                    string
	Mode                     string
	Scope                    string
	MaxDepth                 int
	URLMustMatch             string
	URLMustNotMatch          string
	IndexURLMustMatch        string
	IndexURLMustNotMatch     string
	MaxPagesPerHost          int
	MaxPagesPerRun           int
	AllowQueryURLs           bool
	FollowNoFollowLinks      bool
	NoindexCanonicalMismatch bool
	IgnoreTLSAuthority       bool
	IgnoreRobots             bool
	DisableBrowser           bool
	RecrawlIfOlder           string
	CrawlDelay               string
	// ShowExpert keeps the expert panel open across a redisplay (a validation error
	// or a successful start) when the operator was using it.
	ShowExpert bool
}

type crawlPageData struct {
	AppName      string
	ActivePath   string
	Nav          []NavItem
	CSRF         string
	Section      sectionView
	Form         crawlForm
	Monitor      *crawlMonitorView
	Result       *CrawlDispatch
	Error        string
	Formats      *FormatSettings
	FormatForm   *formatSettingsForm
	FormatsOn    bool
	FormatsError string
	// FormatsNote flashes the outcome of a formats save.
	FormatsNote string
	// Schedules lists the recurring crawls; nil source hides the block.
	Schedules         []CrawlScheduleView
	SchedulesOn       bool
	ScheduleError     string
	SavedProfiles     []SavedCrawlProfileView
	ProfilesOn        bool
	ProfileError      string
	SelectedProfileID string
}

type crawlRunPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Detail     CrawlRunDetail
	Error      string
}

// crawlMonitorView wraps the crawl monitor snapshot with the per-request data the
// control buttons need: the CSRF token and whether control actions are wired.
type crawlMonitorView struct {
	Monitor      CrawlMonitor
	Pagination   CrawlRunPagination
	Health       CrawlHealth
	CSRF         string
	Controllable bool
	Details      bool
}

type configPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Config     ConfigView
	Editable   bool
	Settings   SettingsView
	// SettingGroups is Settings.Items bucketed into ordered categories, one tab
	// each; empty when the settings surface is read-only.
	SettingGroups []SettingGroup
	Bindable      bool
	Bindings      BindingsView
	Notice        string
	Error         string
	// CompactEnabled gates the Storage tab's "Compact now" action; it is set
	// when the console has a storage compactor wired.
	CompactEnabled   bool
	Formats          *FormatSettings
	FormatForm       *formatSettingsForm
	FormatsOn        bool
	FormatsError     string
	FormatsSaveError string
}

type indexPageData struct {
	AppName               string
	ActivePath            string
	Nav                   []NavItem
	CSRF                  string
	Section               sectionView
	Index                 IndexStats
	TermEnabled           bool
	TermQueried           bool
	Term                  TermReport
	Schema                []SchemaGroup
	DocsEnabled           bool
	DocumentDetailEnabled bool
	Documents             DocumentPage
	DocQuery              string
	DocDomain             string
	DeleteEnabled         bool
	RebuildEnabled        bool
	RebuildPending        bool
	RebuildStatusError    string
	RebuildNotice         string
	RebuildError          string
	StorageStatus         indexStorageStatus
	BlacklistEnabled      bool
	Blacklist             []BlacklistEntry
	BlacklistError        bool
	BlacklistProbe        string
}

type securityPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Username   string
	Section    sectionView
	Security   SecurityView
	Pagination SecurityAPIKeyPagination
	Minted     *MintedAPIKey
	Notice     string
	Error      string
}

type activityPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Activity   ActivityView
	Pagination ActivityPagination
}

type performancePageData struct {
	AppName     string
	ActivePath  string
	Nav         []NavItem
	CSRF        string
	Section     sectionView
	Performance PerformanceStatus
	History     []historyView
}

type logsPageData struct {
	AppName    string
	ActivePath string
	Nav        []NavItem
	CSRF       string
	Section    sectionView
	Logs       logsView
}

func csrfToken(r *http.Request) string {
	token, _ := adminauth.CSRFTokenFromContext(r.Context())

	return token
}

type templates struct {
	placeholder   *template.Template
	overview      *template.Template
	search        *template.Template
	activity      *template.Template
	crawl         *template.Template
	crawlRun      *template.Template
	index         *template.Template
	indexDocument *template.Template
	network       *template.Template
	peerDetail    *template.Template
	config        *template.Template
	logs          *template.Template
	security      *template.Template
	performance   *template.Template
	yagorank      *template.Template
	restart       *template.Template
	backup        *template.Template
	portal        *template.Template
}

// Console is the server-rendered admin console handler.
type Console struct {
	mux                  *http.ServeMux
	tpl                  templates
	sections             map[string]sectionView
	overview             OverviewSource
	search               SearchSource
	searchExplanation    SearchExplanationSource
	activity             ActivitySource
	searchNewTab         bool
	crawl                CrawlSource
	crawlFormats         CrawlFormatsSource
	monitor              CrawlMonitorSource
	savedCrawlProfiles   SavedCrawlProfileSource
	crawlRunDetails      CrawlRunDetailSource
	crawlerFetchActivity CrawlerFetchActivitySource
	storagePressure      StoragePressureStatusSource
	schedules            CrawlScheduleSource
	control              CrawlControlSource
	index                IndexSource
	documents            DocumentBrowserSource
	documentDetail       DocumentDetailSource
	indexAdmin           IndexAdminSource
	indexRebuild         IndexRebuildScheduler
	blacklist            BlacklistSource
	indexExport          IndexExporter
	compactor            Compactor
	network              NetworkSource
	networkSelfTest      NetworkSelfTester
	config               ConfigSource
	settings             SettingsSource
	publicSearch         PublicSearchStatusSource
	theme                ThemeStore
	binding              BindingSource
	logs                 LogsSource
	security             SecuritySource
	terms                TermSource
	schema               []SchemaGroup
	ranking              RankingSource
	performance          PerformanceSource
	perfHistory          PerformanceHistorySource
	backup               BackupSource
	peerDetail           PeerDetailSource
	peerNews             PeerNewsSource
	seedlistRefresh      SeedlistRefreshSource
	peerBlock            PeerBlockSource
	restart              func()
	restartCrawlers      func() int
	searchSuggest        http.Handler
	publicBase           string
	publicPort           string
}

// New builds the console with its embedded templates, assets, and providers.
func New(opts Options) *Console {
	// assetFS embeds the "assets" directory under a valid path, so fs.Sub
	// always resolves it; the returned error is structurally unreachable.
	assets, _ := fs.Sub(assetFS, "assets")

	console := &Console{
		mux:                  http.NewServeMux(),
		tpl:                  buildTemplates(),
		sections:             defaultSections(),
		overview:             opts.Overview,
		search:               opts.Search,
		searchExplanation:    opts.SearchExplanation,
		activity:             opts.Activity,
		searchNewTab:         opts.SearchLinksNewTab,
		searchSuggest:        opts.SearchSuggest,
		crawl:                opts.Crawl,
		crawlFormats:         opts.CrawlFormats,
		monitor:              opts.Monitor,
		crawlerFetchActivity: opts.CrawlerFetchActivity,
		storagePressure:      opts.StoragePressure,
		schedules:            opts.Schedules,
		savedCrawlProfiles:   opts.SavedCrawlProfiles,
		crawlRunDetails:      opts.CrawlRunDetails,
		control:              opts.Control,
		index:                opts.Index,
		documents:            opts.Documents,
		documentDetail:       opts.DocumentDetail,
		indexAdmin:           opts.IndexAdmin,
		indexRebuild:         opts.IndexRebuild,
		blacklist:            opts.Blacklist,
		indexExport:          opts.IndexExport,
		compactor:            opts.Compactor,
		network:              opts.Network,
		networkSelfTest:      opts.NetworkSelfTest,
		config:               opts.Config,
		settings:             opts.Settings,
		publicSearch:         opts.PublicSearch,
		theme:                opts.Theme,
		binding:              opts.Binding,
		logs:                 opts.Logs,
		security:             opts.Security,
		terms:                opts.Terms,
		schema:               opts.Schema,
		ranking:              opts.Ranking,
		performance:          opts.Performance,
		perfHistory:          opts.PerformanceHistory,
		backup:               opts.Backup,
		peerDetail:           opts.PeerDetail,
		peerNews:             opts.PeerNews,
		seedlistRefresh:      opts.SeedlistRefresh,
		peerBlock:            opts.PeerBlock,
		restart:              opts.Restart,
		restartCrawlers:      opts.RestartCrawlers,
		publicBase:           strings.TrimRight(opts.PublicBaseURL, "/"),
		publicPort:           publicListenerPort(opts.PublicAddr),
	}
	console.registerRoutes(assets)

	return console
}

// publicListenerPort extracts the port the public search portal listens on so
// the header link can be built from the request host. A disabled or portless
// address yields "", which hides the derived link.
func publicListenerPort(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}

	return ""
}

func buildTemplates() templates {
	layout := template.Must(
		template.New("layout.tmpl").Funcs(adminAssetTemplateFunctions()).ParseFS(
			templateFS,
			"templates/layout.tmpl",
			"templates/system_monitor.tmpl",
		),
	)
	clone := func(fns template.FuncMap, files ...string) *template.Template {
		return template.Must(template.Must(layout.Clone()).Funcs(fns).ParseFS(templateFS, files...))
	}

	return templates{
		placeholder: clone(nil, "templates/placeholder.tmpl"),
		overview:    clone(overviewFuncs, "templates/overview.tmpl", "templates/metrics.tmpl"),
		search:      clone(nil, "templates/search.tmpl"),
		activity:    clone(nil, "templates/activity.tmpl"),
		crawl: clone(
			crawlFuncs,
			"templates/crawl.tmpl",
			"templates/crawl_monitor.tmpl",
			"templates/crawl_formats.tmpl",
		),
		crawlRun:      clone(nil, "templates/crawl_run.tmpl"),
		index:         clone(nil, "templates/index.tmpl"),
		indexDocument: clone(nil, "templates/index_document.tmpl"),
		network:       clone(nil, "templates/network.tmpl"),
		peerDetail:    clone(nil, "templates/peer_detail.tmpl"),
		config: clone(
			nil,
			"templates/config.tmpl",
			"templates/toasts.tmpl",
			"templates/crawl_formats.tmpl",
		),
		logs:        clone(nil, "templates/logs.tmpl", "templates/logs_table.tmpl"),
		security:    clone(nil, "templates/security.tmpl", "templates/toasts.tmpl"),
		performance: clone(nil, "templates/performance.tmpl"),
		yagorank:    clone(nil, "templates/yagorank.tmpl", "templates/toasts.tmpl"),
		restart:     clone(nil, "templates/restart.tmpl"),
		backup:      clone(nil, "templates/backup.tmpl"),
		portal:      clone(nil, "templates/portal.tmpl"),
	}
}

func (c *Console) registerRoutes(assets fs.FS) {
	c.registerAssetAndSearchRoutes(assets)
	c.registerCrawlRoutes()
	c.registerIndexAndNetworkRoutes()
	c.registerOperatorRoutes()
	c.registerStaticSectionRoutes()
}

func dynamicSection(path string) bool {
	return path == overviewPath || path == searchPath || path == crawlPath ||
		path == indexPath || path == networkPath || path == configPath ||
		path == logsPath || path == securityPath || path == performancePath ||
		path == yagorankPath || path == activityPath ||
		path == restartPath || path == portalPath || path == backupPath
}

// ServeHTTP dispatches to the console's internal router, first resolving the
// public search portal's address for this request so the shared layout can link
// to it without every page threading the value through its own data.
func (c *Console) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rejectAdminAssetAlias(w, r) {
		return
	}
	ctx := context.WithValue(r.Context(), publicSearchHrefKey{}, c.publicSearchHref(r))
	c.mux.ServeHTTP(w, r.WithContext(ctx))
}

// publicSearchHrefKey types the context value carrying the resolved link.
type publicSearchHrefKey struct{}

// publicSearchHref resolves the URL of the public search portal: the configured
// public origin when set, otherwise the request's own host with the public
// listener's port. It is empty when the public surface is disabled, which hides
// the header link rather than pointing it at the admin origin.
func (c *Console) publicSearchHref(r *http.Request) string {
	publicBase := c.publicBase
	if c.publicSearch != nil {
		status := c.publicSearch.PublicSearchStatus(r.Context())
		if !status.Enabled {
			return ""
		}
		publicBase = strings.TrimRight(status.BaseURL, "/")
	}
	if publicBase != "" {
		return publicBase
	}
	if c.publicPort == "" || r.Host == "" {
		return ""
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}

	return scheme + "://" + net.JoinHostPort(host, c.publicPort) + "/"
}

// publicSearchHrefFromContext reads the resolved link, empty when absent.
func publicSearchHrefFromContext(ctx context.Context) string {
	href, _ := ctx.Value(publicSearchHrefKey{}).(string)

	return href
}

// layoutEnvelope wraps a page's data with the request-scoped chrome the shared
// layout needs but the page data does not carry, so a new chrome value does not
// force a field onto every page-data struct.
type layoutEnvelope struct {
	PublicSearchHref string
	SystemMonitor    systemMonitorView
	Data             any
}

func (c *Console) handleSystemMonitor(w http.ResponseWriter, r *http.Request) {
	c.render(
		r.Context(),
		w,
		c.tpl.placeholder,
		"system-monitor",
		buildSystemMonitorWithCrawler(c.perfHistory, c.crawlerFetchActivity),
	)
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
	c.renderIndexPage(w, r, indexNotes{})
}

// indexNotes carries one-shot messages the Index page shows after an action.
type indexNotes struct {
	BlacklistProbe string
	RebuildNotice  string
	RebuildError   string
}

func (c *Console) renderIndexPage(w http.ResponseWriter, r *http.Request, notes indexNotes) {
	if c.index == nil {
		c.renderUnavailable(w, r, indexPath, "Index", indexUnavailable)

		return
	}

	data := indexPageData{
		AppName: appName, ActivePath: indexPath, Nav: navItems,
		CSRF:                  csrfToken(r),
		Section:               sectionView{Heading: "Index", Available: true},
		Index:                 c.index.Index(r.Context()),
		Schema:                c.schema,
		DocumentDetailEnabled: c.documentDetail != nil,
		RebuildEnabled:        c.indexRebuild != nil,
		RebuildNotice:         notes.RebuildNotice,
		RebuildError:          notes.RebuildError,
		StorageStatus: buildIndexStorageStatus(
			c.crawlerFetchActivity,
			c.storagePressure,
		),
	}
	if c.indexRebuild != nil {
		pending, err := c.indexRebuild.RebuildPending(r.Context())
		data.RebuildPending = pending
		if err != nil {
			data.RebuildStatusError = "Rebuild status is unavailable."
		}
	}
	if c.terms != nil {
		data.TermEnabled = true
		if term := strings.TrimSpace(r.URL.Query().Get("term")); term != "" {
			data.TermQueried = true
			data.Term = c.terms.LookupTerm(r.Context(), term)
		}
	}
	if c.documents != nil {
		data.DocsEnabled = true
		data.DeleteEnabled = c.indexAdmin != nil
		data.DocQuery = strings.TrimSpace(r.URL.Query().Get("q"))
		data.DocDomain = strings.TrimSpace(r.URL.Query().Get("domain"))
		data.Documents = c.documents.BrowseDocuments(r.Context(), DocumentQuery{
			URLContains: data.DocQuery,
			Domain:      data.DocDomain,
		})
	}
	if c.blacklist != nil {
		data.BlacklistEnabled = true
		var err error
		data.Blacklist, err = c.blacklist.BlacklistEntries(r.Context())
		data.BlacklistError = err != nil
		data.BlacklistProbe = notes.BlacklistProbe
	}

	c.render(r.Context(), w, c.tpl.index, "layout", data)
}

func (c *Console) handleBlacklist(w http.ResponseWriter, r *http.Request) {
	if c.blacklist == nil {
		http.NotFound(w, r)

		return
	}

	ctx := r.Context()
	action := r.PostFormValue("action")
	kind := strings.TrimSpace(r.PostFormValue("kind"))
	value := strings.TrimSpace(r.PostFormValue("value"))
	var err error
	switch action {
	case "add":
		err = c.blacklist.AddBlacklist(ctx, kind, value)
	case "remove":
		err = c.blacklist.RemoveBlacklist(ctx, kind, value)
	default:
		http.Error(w, "unknown blacklist action", http.StatusBadRequest)

		return
	}
	if err != nil {
		slog.WarnContext(ctx, "admin blacklist action failed",
			slog.String("action", action), slog.Any("error", err))
	}

	http.Redirect(w, r, indexPath, http.StatusSeeOther)
}

// handleBlacklistTest answers the "would this URL be blocked" probe (UI-17,
// YaCy BlacklistTest_p) by re-rendering the Index page with the verdict.
func (c *Console) handleBlacklistTest(w http.ResponseWriter, r *http.Request) {
	prober, ok := c.blacklist.(BlacklistProber)
	if c.blacklist == nil || !ok {
		http.NotFound(w, r)

		return
	}
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	probe := ""
	switch blocked, err := prober.BlacklistBlocks(r.Context(), rawURL); {
	case rawURL == "":
		probe = "Enter a URL to test."
	case err != nil:
		probe = "Probe failed: " + err.Error()
	case blocked:
		probe = rawURL + " is BLOCKED by the denylist."
	default:
		probe = rawURL + " is not blocked."
	}
	c.renderIndexPage(w, r, indexNotes{BlacklistProbe: probe})
}

// handleBlacklistExport streams the denylist as importable plaintext (UI-17,
// YaCy BlacklistImpExp_p).
func (c *Console) handleBlacklistExport(w http.ResponseWriter, r *http.Request) {
	porter, ok := c.blacklist.(BlacklistPorter)
	if c.blacklist == nil || !ok {
		http.NotFound(w, r)

		return
	}
	payload, err := porter.ExportBlacklist(r.Context())
	if err != nil {
		http.Error(w, "export failed", http.StatusInternalServerError)

		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="denylist.txt"`)
	// Plaintext export of operator-entered denylist lines; nothing renders
	// as markup.
	// nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter
	_, _ = w.Write([]byte(payload))
}

// handleBlacklistImport adds every line of the pasted payload.
func (c *Console) handleBlacklistImport(w http.ResponseWriter, r *http.Request) {
	porter, ok := c.blacklist.(BlacklistPorter)
	if c.blacklist == nil || !ok {
		http.NotFound(w, r)

		return
	}
	added, err := porter.ImportBlacklist(r.Context(), r.PostFormValue("payload"))
	note := fmt.Sprintf("Imported %d entries.", added)
	if err != nil {
		note = fmt.Sprintf("Imported %d entries, then failed: %v", added, err)
	}
	c.renderIndexPage(w, r, indexNotes{BlacklistProbe: note})
}

// handleIndexExport streams the filtered corpus in the requested format
// (UI-18, YaCy IndexExport_p).
func (c *Console) handleIndexExport(w http.ResponseWriter, r *http.Request) {
	if c.indexExport == nil {
		http.NotFound(w, r)

		return
	}
	req := IndexExportRequest{
		Format:      strings.TrimSpace(r.URL.Query().Get("format")),
		Domain:      strings.TrimSpace(r.URL.Query().Get("domain")),
		URLContains: strings.TrimSpace(r.URL.Query().Get("q")),
	}
	contentType, filename, ok := exportContentMeta(req.Format)
	if !ok {
		http.Error(w, "unknown export format", http.StatusBadRequest)

		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if err := c.indexExport.ExportDocuments(r.Context(), req, w); err != nil {
		slog.WarnContext(r.Context(), "index export failed", slog.Any("error", err))
	}
}

// exportContentMeta maps an export format onto its response headers.
func exportContentMeta(format string) (contentType, filename string, ok bool) {
	switch format {
	case "", "text":
		return "text/plain; charset=utf-8", "index-urls.txt", true
	case "csv":
		return "text/csv; charset=utf-8", "index-export.csv", true
	case "jsonl":
		return "application/x-ndjson", "index-export.jsonl", true
	default:
		return "", "", false
	}
}

func (c *Console) handleIndexDelete(w http.ResponseWriter, r *http.Request) {
	if c.indexAdmin == nil {
		http.NotFound(w, r)

		return
	}

	ctx := r.Context()
	var err error
	switch r.PostFormValue("action") {
	case "url":
		err = c.indexAdmin.DeleteDocument(ctx, strings.TrimSpace(r.PostFormValue("url")))
	case "domain":
		_, err = c.indexAdmin.DeleteDomain(ctx, strings.TrimSpace(r.PostFormValue("domain")))
	default:
		http.Error(w, "unknown index delete action", http.StatusBadRequest)

		return
	}
	if err != nil {
		slog.WarnContext(ctx, "admin index delete failed",
			slog.String("action", r.PostFormValue("action")), slog.Any("error", err))
	}

	http.Redirect(w, r, indexPath, http.StatusSeeOther)
}

func (c *Console) handleActivity(w http.ResponseWriter, r *http.Request) {
	if c.activity == nil {
		c.renderUnavailable(w, r, activityPath, "Activity", activityUnavailable)

		return
	}

	activity := c.activity.Activity(r.Context())
	c.render(r.Context(), w, c.tpl.activity, "layout", activityPageData{
		AppName: appName, ActivePath: activityPath, Nav: navItems,
		CSRF:       csrfToken(r),
		Section:    sectionView{Heading: "Search activity", Available: true},
		Activity:   activity,
		Pagination: buildActivityPagination(activity.Entries, r.FormValue("apage")),
	})
}

func (c *Console) handlePerformance(w http.ResponseWriter, r *http.Request) {
	if c.performance == nil {
		c.renderUnavailable(w, r, performancePath, "Performance", performanceUnavailable)

		return
	}

	c.render(r.Context(), w, c.tpl.performance, "layout", performancePageData{
		AppName: appName, ActivePath: performancePath, Nav: navItems,
		CSRF:        csrfToken(r),
		Section:     sectionView{Heading: "Performance", Available: true},
		Performance: c.performance.Performance(r.Context()),
		History:     performanceHistory(c.perfHistory),
	})
}

func (c *Console) handleLogs(w http.ResponseWriter, r *http.Request) {
	if c.logs == nil {
		c.renderUnavailable(w, r, logsPath, "Logs", logsUnavailable)

		return
	}

	c.render(r.Context(), w, c.tpl.logs, "layout", logsPageData{
		AppName: appName, ActivePath: logsPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Logs", Available: true},
		Logs:    c.logsView(r),
	})
}

func (c *Console) handleLogsEvents(w http.ResponseWriter, r *http.Request) {
	if c.logs == nil {
		http.NotFound(w, r)

		return
	}

	c.render(r.Context(), w, c.tpl.logs, "logs-table", c.logsView(r))
}

// logsView builds the Logs render model, applying the severity and category
// filters from the query string while offering the full category vocabulary.
func (c *Console) logsView(r *http.Request) logsView {
	entries := c.logs.Logs(r.Context())
	severity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("severity")))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	needle := strings.TrimSpace(r.URL.Query().Get("q"))
	fromValue := strings.TrimSpace(r.URL.Query().Get("from"))
	toValue := strings.TrimSpace(r.URL.Query().Get("to"))
	timeRange, filterError := validatedLogTimeRange(fromValue, toValue)
	filtered := filterLogEntries(entries, severity, category, needle)
	if filterError == "" {
		filtered = filterLogEntriesInTimeRange(filtered, timeRange)
	} else {
		filtered = nil
	}

	return logsView{
		Entries:     filtered,
		Severity:    severity,
		Category:    category,
		Query:       needle,
		From:        fromValue,
		To:          toValue,
		FilterError: filterError,
		Severities:  logSeverities,
		Categories:  distinctLogCategories(entries),
	}
}

func (c *Console) handleNetwork(w http.ResponseWriter, r *http.Request) {
	if c.network == nil {
		c.renderUnavailable(w, r, networkPath, "Network", networkUnavailable)

		return
	}

	status := c.network.Network(r.Context())
	selfTest := networkSelfTestPageFromContext(r.Context())
	if selfTest.status != nil {
		status = *selfTest.status
	}
	peerNews, peerNewsAvailable := c.peerNewsSnapshot(r.Context())
	query := r.URL.Query()
	peerTable := buildPeerTable(
		status.Peers,
		query.Get("psort"),
		query.Get("pdir"),
		query.Get("ppage"),
	)

	c.render(r.Context(), w, c.tpl.network, "layout", pageData{
		AppName: appName, ActivePath: networkPath, Nav: navItems,
		CSRF:                   csrfToken(r),
		Section:                sectionView{Heading: "Network", Available: true},
		Network:                status,
		NetworkSelfTestEnabled: c.networkSelfTest != nil,
		NetworkSelfTestNotice:  selfTest.notice,
		NetworkSelfTestError:   selfTest.err,
		PeerTable:              peerTable,
		PeerLinks:              c.peerDetail != nil,
		PeerNews:               peerNews,
		PeerNewsEnabled:        c.peerNews != nil,
		PeerNewsAvailable:      peerNewsAvailable,
		SeedlistRefresh:        c.seedlistRefresh != nil,
	})
}

func (c *Console) handleSeedlistRefresh(w http.ResponseWriter, r *http.Request) {
	if c.seedlistRefresh == nil {
		http.NotFound(w, r)

		return
	}

	url := strings.TrimSpace(r.PostFormValue("url"))
	if err := c.seedlistRefresh.RefreshSeedlist(r.Context(), url); err != nil {
		slog.WarnContext(r.Context(), "admin seedlist refresh failed",
			slog.String("url", url), slog.Any("error", err))
	}

	http.Redirect(w, r, networkPath, http.StatusSeeOther)
}

func (c *Console) peerNewsSnapshot(ctx context.Context) ([]PeerNewsItem, bool) {
	if c.peerNews == nil {
		return nil, false
	}

	return c.peerNews.PeerNews(ctx)
}

func (c *Console) handleNetworkPeer(w http.ResponseWriter, r *http.Request) {
	if c.peerDetail == nil {
		http.NotFound(w, r)

		return
	}

	detail, ok, err := c.peerDetail.PeerDetail(r.Context(), r.URL.Query().Get("hash"))
	if err != nil {
		slog.WarnContext(r.Context(), "admin peer detail failed", slog.Any("error", err))
		c.renderUnavailable(w, r, networkPath, "Peer detail", peerDetailUnavailable)

		return
	}
	if !ok {
		http.NotFound(w, r)

		return
	}

	c.render(r.Context(), w, c.tpl.peerDetail, "layout", peerDetailPageData{
		AppName: appName, ActivePath: networkPath, Nav: navItems,
		CSRF:         csrfToken(r),
		Section:      sectionView{Heading: "Peer detail", Available: true},
		Peer:         detail,
		BlockEnabled: c.peerBlock != nil && detail.BlockStatusKnown,
	})
}

func (c *Console) handlePeerBlock(w http.ResponseWriter, r *http.Request) {
	if c.peerBlock == nil {
		http.NotFound(w, r)

		return
	}

	hash := strings.TrimSpace(r.PostFormValue("hash"))
	action := r.PostFormValue("action")
	var err error
	switch action {
	case "block":
		err = c.peerBlock.Block(r.Context(), hash)
	case "unblock":
		err = c.peerBlock.Unblock(r.Context(), hash)
	default:
		http.Error(w, "unknown peer block action", http.StatusBadRequest)

		return
	}
	if err != nil {
		slog.WarnContext(r.Context(), "admin peer block action failed",
			slog.String("action", action), slog.String("hash", hash), slog.Any("error", err))
	}

	// Redirect to the static Network list (never a user-supplied URL) where the
	// peer's new blocked state is visible.
	http.Redirect(w, r, networkPath, http.StatusSeeOther)
}

func (c *Console) handleConfig(w http.ResponseWriter, r *http.Request) {
	if c.config == nil {
		c.renderUnavailable(w, r, configPath, "Configuration", configUnavailable)

		return
	}

	notice := ""
	if r.URL.Query().Get("saved") == "formats" {
		notice = "Document format settings updated."
	}
	c.render(r.Context(), w, c.tpl.config, "layout", c.configPage(r, notice, ""))
}

func (c *Console) handleConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if c.config == nil {
		c.renderUnavailable(w, r, configPath, "Configuration", configUnavailable)

		return
	}

	if r.PostFormValue("form") == "compact" {
		c.handleConfigCompact(w, r)

		return
	}

	binding := r.PostFormValue("form") == "binding"
	if (binding && c.binding == nil) || (!binding && c.settings == nil) {
		http.NotFound(w, r)

		return
	}

	// Config's settings batch runs with a nil gate (the source rejects unknown
	// keys), so it never signals a foreign-key 404 here; ok is discarded.
	notice, errMsg, _ := c.applyConfigUpdate(r, binding)
	c.render(r.Context(), w, c.tpl.config, "layout", c.configPage(r, notice, errMsg))
}

// handleConfigCompact runs an on-demand storage compaction from the Storage tab
// and re-renders Configuration with the reclaimed-space toast. Compaction is
// idempotent, so a resubmit reclaims nothing rather than harming the store.
func (c *Console) handleConfigCompact(w http.ResponseWriter, r *http.Request) {
	if c.compactor == nil {
		http.NotFound(w, r)

		return
	}

	notice, errMsg := "", ""
	result, err := c.compactor.Compact(r.Context())
	if err != nil {
		slog.WarnContext(r.Context(), "admin storage compaction failed", slog.Any("error", err))
		var operatorError CompactionOperatorError
		if errors.As(err, &operatorError) {
			errMsg = operatorError.CompactionOperatorMessage()
		} else {
			errMsg = "Compaction failed."
		}
	} else {
		notice = compactionNotice(result)
	}
	c.render(r.Context(), w, c.tpl.config, "layout", c.configPage(r, notice, errMsg))
}

// compactionNotice renders the operator-facing summary of a compaction run.
func compactionNotice(result CompactionResult) string {
	if result.ShardsCompacted == 0 {
		return "Storage is already compact — nothing to reclaim."
	}

	return fmt.Sprintf(
		"Reclaimed %s across %d shard(s).",
		result.BytesReclaimed, result.ShardsCompacted,
	)
}

func (c *Console) applyConfigUpdate(
	r *http.Request,
	binding bool,
) (notice, errMsg string, ok bool) {
	if binding {
		result, err := c.binding.UpdateBinding(r.Context(), parseBindChange(r))
		if err != nil {
			slog.WarnContext(r.Context(), "admin bind update failed", slog.Any("error", err))
		}
		notice, errMsg = bindingOutcome(result, err)

		return notice, errMsg, true
	}

	return c.applySettingsBatch(r, nil)
}

func bindingOutcome(result BindResult, err error) (notice, errMsg string) {
	switch {
	case err != nil:
		return "", "Update failed. Please try again."
	case !result.OK:
		return "", result.Message
	default:
		return result.Message, ""
	}
}

func parseBindChange(r *http.Request) BindChange {
	action := BindAction(strings.TrimSpace(r.PostFormValue("binding_action")))
	if action == "" {
		action = BindActionSet
	}

	return BindChange{
		Key:    strings.TrimSpace(r.PostFormValue("key")),
		Host:   strings.TrimSpace(r.PostFormValue("host")),
		Port:   strings.TrimSpace(r.PostFormValue("port")),
		Action: action,
	}
}

func settingsOutcome(result SettingsResult, err error) (notice, errMsg string) {
	switch {
	case err != nil:
		return "", "Update failed. Please try again."
	case !result.OK:
		return "", result.Message
	default:
		notice = result.Message
		if result.RestartRequired {
			notice += " Restart the node for the change to take effect."
		}

		return notice, ""
	}
}

func (c *Console) configPage(r *http.Request, notice, errMsg string) configPageData {
	data := configPageData{
		AppName: appName, ActivePath: configPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Configuration", Available: true},
		Config:  c.config.Config(r.Context()),
		Notice:  notice,
		Error:   errMsg,
	}
	if c.settings != nil {
		data.Editable = true
		data.Settings = c.settings.Settings(r.Context())
		data.SettingGroups = configurationSettingGroups(
			withoutPortalCategory(data.Settings.Items),
		)
	}
	if c.binding != nil {
		data.Bindable = true
		data.Bindings = c.binding.Bindings(r.Context())
	}
	data.CompactEnabled = c.compactor != nil
	c.loadConfigFormats(r, &data)

	return data
}

func (c *Console) handleSecurity(w http.ResponseWriter, r *http.Request) {
	if c.security == nil {
		c.renderUnavailable(w, r, securityPath, "Security", securityUnavailable)

		return
	}

	c.render(r.Context(), w, c.tpl.security, "layout", c.securityPage(r, "", "", nil))
}

func (c *Console) handleSecurityUpdate(w http.ResponseWriter, r *http.Request) {
	if c.security == nil {
		c.renderUnavailable(w, r, securityPath, "Security", securityUnavailable)

		return
	}

	notice, errMsg, minted := c.applySecurityUpdate(r)
	c.render(r.Context(), w, c.tpl.security, "layout", c.securityPage(r, notice, errMsg, minted))
}

func (c *Console) applySecurityUpdate(
	r *http.Request,
) (notice, errMsg string, minted *MintedAPIKey) {
	switch r.PostFormValue("form") {
	case "mint":
		return c.applyAPIKeyMint(r)
	case "revoke":
		notice, errMsg = c.applyAPIKeyRevoke(r)

		return notice, errMsg, nil
	case "password":
		notice, errMsg = c.applyPasswordChange(r)

		return notice, errMsg, nil
	default:
		return "", "Unknown action.", nil
	}
}

func (c *Console) applyAPIKeyMint(
	r *http.Request,
) (notice, errMsg string, minted *MintedAPIKey) {
	result, err := c.security.MintAPIKey(r.Context(), parseAPIKeyMint(r))
	if err != nil {
		slog.WarnContext(r.Context(), "admin api-key mint failed", slog.Any("error", err))
	}
	if err == nil && result.OK {
		minted = result.Created
	}
	notice, errMsg = writeOutcome(
		result.OK, result.Message, err, "Could not create the API key. Please try again.",
	)

	return notice, errMsg, minted
}

func (c *Console) applyAPIKeyRevoke(r *http.Request) (notice, errMsg string) {
	revoke := APIKeyRevoke{ID: strings.TrimSpace(r.PostFormValue("id"))}
	result, err := c.security.RevokeAPIKey(r.Context(), revoke)
	if err != nil {
		slog.WarnContext(r.Context(), "admin api-key revoke failed", slog.Any("error", err))
	}

	return writeOutcome(
		result.OK, result.Message, err, "Could not revoke the API key. Please try again.",
	)
}

func (c *Console) applyPasswordChange(r *http.Request) (notice, errMsg string) {
	result, err := c.security.ChangePassword(r.Context(), parsePasswordChange(r))
	if err != nil {
		slog.WarnContext(r.Context(), "admin password change failed", slog.Any("error", err))
	}

	return writeOutcome(
		result.OK, result.Message, err, "Could not change the password. Please try again.",
	)
}

func writeOutcome(ok bool, message string, err error, failMsg string) (notice, errMsg string) {
	switch {
	case err != nil:
		return "", failMsg
	case !ok:
		return "", message
	default:
		return message, ""
	}
}

func parseAPIKeyMint(r *http.Request) APIKeyMint {
	_ = r.ParseForm()

	return APIKeyMint{
		Label:  strings.TrimSpace(r.PostFormValue("label")),
		Scopes: r.PostForm["scope"],
	}
}

func parsePasswordChange(r *http.Request) PasswordChange {
	return PasswordChange{
		Current: r.PostFormValue("current"),
		New:     r.PostFormValue("new"),
		Confirm: r.PostFormValue("confirm"),
	}
}

func (c *Console) securityPage(
	r *http.Request,
	notice, errMsg string,
	minted *MintedAPIKey,
) securityPageData {
	username, _ := adminauth.PrincipalFromContext(r.Context())
	cursor := r.URL.Query().Get(securityAccessCursorParameter)
	security := c.security.Security(r.Context(), SecurityAPIKeyPageRequest{Cursor: cursor})

	return securityPageData{
		AppName: appName, ActivePath: securityPath, Nav: navItems,
		CSRF: csrfToken(r), Username: username,
		Section:  sectionView{Heading: "Security", Available: true},
		Security: security,
		Pagination: buildSecurityAPIKeyPagination(
			security,
			cursor,
			r.URL.Query().Get(securityAccessHistoryParameter),
		),
		Minted: minted,
		Notice: notice,
		Error:  errMsg,
	}
}

func (c *Console) handleSearch(w http.ResponseWriter, r *http.Request) {
	if c.search == nil {
		c.renderUnavailable(w, r, searchPath, "Search", searchUnavailable)

		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	global := r.URL.Query().Get("scope") != "local"
	filters := searchFiltersFromValues(r.URL.Query())
	page := parseSearchPage(r.URL.Query().Get("p"))
	data := searchPageData{
		AppName: appName, ActivePath: searchPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Search", Available: true},
		Query:   query, Global: global, Filters: filters,
		ContentDomains: searchContentDomainOptions,
		NewTab:         c.searchNewTab,
		SuggestEnabled: c.searchSuggest != nil,
	}
	if query != "" && c.searchExplanation != nil {
		data.ExplainURL = searchExplanationURL(query, global)
	}

	if query != "" {
		data.Submitted = true
		offset := (page - 1) * adminSearchPageSize
		results, err := c.search.Search(r.Context(), SearchQuery{
			Query: query, Global: global, Filters: filters,
			Offset: offset, Limit: adminSearchPageSize,
		})
		if err != nil {
			slog.WarnContext(r.Context(), "admin search failed", slog.Any("error", err))
			data.Error = "Search failed."
		} else {
			if canonicalPage, redirect := canonicalAdminSearchPage(
				page,
				len(results.Results),
				results.TotalResults,
			); redirect {
				redirectFilteredAdminSearchPage(w, query, global, filters, canonicalPage)

				return
			}
			data.Results = results
			data.Pagination = newSearchPagination(searchPaginationWindow{
				query: query, global: global, filters: filters, page: page,
				shown: len(results.Results), total: results.TotalResults,
			})
		}
	}

	c.render(r.Context(), w, c.tpl.search, "layout", data)
}

// parseSearchPage reads the 1-based ?p= page number, clamping junk and
// out-of-range values into [1, adminSearchMaxPage].
func parseSearchPage(raw string) int {
	page, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || page < 1 {
		return 1
	}
	if page > adminSearchMaxPage {
		return adminSearchMaxPage
	}

	return page
}

// newSearchPagination decides which navigation links to show. A next link
// appears only while more results remain than the current window covers and the
// page cap is not reached; a previous link appears past the first page.
func newSearchPagination(
	window searchPaginationWindow,
) SearchPagination {
	offset := (window.page - 1) * adminSearchPageSize
	nav := SearchPagination{
		Page:    window.page,
		HasPrev: window.page > 1,
		HasNext: offset+window.shown < window.total && window.page < adminSearchMaxPage,
	}
	if nav.HasPrev {
		nav.PrevURL = filteredAdminSearchPageURL(
			window.query,
			window.global,
			window.filters,
			window.page-1,
		)
	}
	if nav.HasNext {
		nav.NextURL = filteredAdminSearchPageURL(
			window.query,
			window.global,
			window.filters,
			window.page+1,
		)
	}

	return nav
}

type searchPaginationWindow struct {
	query   string
	global  bool
	filters SearchFilters
	page    int
	shown   int
	total   int
}

func (c *Console) handleCrawl(w http.ResponseWriter, r *http.Request) {
	if c.crawl == nil {
		c.renderUnavailable(w, r, crawlPath, "Crawler", crawlUnavailable)

		return
	}

	form, err := c.selectedSavedCrawlForm(r)
	data := c.crawlPage(r, form)
	if err != nil {
		data.ProfileError = err.Error()
	}
	c.render(r.Context(), w, c.tpl.crawl, "layout", data)
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
		Name:                     form.Name,
		Seeds:                    seeds,
		Mode:                     form.Mode,
		Scope:                    form.Scope,
		MaxDepth:                 form.MaxDepth,
		URLMustMatch:             form.URLMustMatch,
		URLMustNotMatch:          form.URLMustNotMatch,
		IndexURLMustMatch:        form.IndexURLMustMatch,
		IndexURLMustNotMatch:     form.IndexURLMustNotMatch,
		MaxPagesPerHost:          form.MaxPagesPerHost,
		MaxPagesPerRun:           &form.MaxPagesPerRun,
		AllowQueryURLs:           form.AllowQueryURLs,
		FollowNoFollowLinks:      form.FollowNoFollowLinks,
		NoindexCanonicalMismatch: form.NoindexCanonicalMismatch,
		IgnoreTLSAuthority:       form.IgnoreTLSAuthority,
		IgnoreRobots:             form.IgnoreRobots,
		DisableBrowser:           form.DisableBrowser,
		RecrawlIfOlder:           form.RecrawlIfOlder,
		CrawlDelay:               form.CrawlDelay,
	})
	if err != nil {
		slog.WarnContext(r.Context(), "admin crawl start failed", slog.Any("error", err))
		// The dispatcher validates the profile (scope, durations, and the four
		// regex filters), so surfacing its message tells the operator which expert
		// field to fix rather than a generic failure.
		data.Error = "Crawl start failed: " + err.Error()
	} else {
		data.Result = &result
	}

	c.render(r.Context(), w, c.tpl.crawl, "layout", data)
}

func (c *Console) crawlPage(r *http.Request, form crawlForm) crawlPageData {
	data := crawlPageData{
		AppName: appName, ActivePath: crawlPath, Nav: navItems,
		CSRF:    csrfToken(r),
		Section: sectionView{Heading: "Crawler", Available: true},
		Form:    form,
		Monitor: c.crawlMonitorView(r),
	}
	if c.crawlFormats != nil {
		data.FormatsOn = true
		settings, err := c.crawlFormats.CurrentFormats(r.Context())
		if err != nil {
			slog.WarnContext(
				r.Context(),
				"load admin crawl formats failed",
				slog.Any("error", err),
			)
			data.FormatsError = "Document format settings are unavailable."
		} else {
			data.Formats = &settings
			data.FormatForm = &formatSettingsForm{
				Action:   crawlPath + "/formats",
				CSRF:     data.CSRF,
				Settings: settings,
			}
		}
	}
	if c.schedules != nil {
		data.SchedulesOn = true
		schedules, err := c.schedules.Schedules(r.Context())
		if err != nil {
			slog.WarnContext(
				r.Context(),
				"load admin crawl schedules failed",
				slog.Any("error", err),
			)
			data.ScheduleError = "Crawl schedules are unavailable."
		} else {
			data.Schedules = schedules
		}
	}
	if c.savedCrawlProfiles != nil {
		data.ProfilesOn = true
		data.SelectedProfileID = strings.TrimSpace(r.FormValue("profile"))
		profiles, err := c.savedCrawlProfiles.Profiles(r.Context())
		if err != nil {
			data.ProfileError = "Saved crawl profiles are unavailable."
		} else {
			data.SavedProfiles = profiles
		}
	}

	return data
}

// handleCrawlSchedule serves the recurring-crawl actions (UI-19).
func (c *Console) handleCrawlSchedule(w http.ResponseWriter, r *http.Request) {
	if c.schedules == nil {
		http.NotFound(w, r)

		return
	}
	ctx := r.Context()
	var err error
	switch r.PostFormValue("action") {
	case "create":
		depth, _ := strconv.Atoi(r.PostFormValue("maxDepth"))
		err = c.schedules.CreateSchedule(ctx, CrawlScheduleRequest{
			Name:     strings.TrimSpace(r.PostFormValue("name")),
			Seeds:    strings.Split(r.PostFormValue("seeds"), "\n"),
			Scope:    r.PostFormValue("scope"),
			MaxDepth: depth,
			Interval: r.PostFormValue("interval"),
		})
	case "delete":
		err = c.schedules.DeleteSchedule(ctx, r.PostFormValue("id"))
	case "enable":
		err = c.schedules.SetScheduleEnabled(ctx, r.PostFormValue("id"), true)
	case "disable":
		err = c.schedules.SetScheduleEnabled(ctx, r.PostFormValue("id"), false)
	default:
		http.Error(w, "unknown schedule action", http.StatusBadRequest)

		return
	}
	if err != nil {
		data := c.crawlPage(r, defaultCrawlFormFor(c.crawl))
		data.ScheduleError = err.Error()
		c.render(ctx, w, c.tpl.crawl, "layout", data)

		return
	}
	http.Redirect(w, r, crawlPath, http.StatusSeeOther)
}

// handleCrawlFormats saves the shared document-format toggles.
func (c *Console) handleCrawlFormats(w http.ResponseWriter, r *http.Request) {
	if c.crawlFormats == nil {
		http.NotFound(w, r)

		return
	}
	settings, err := formatSettingsFromRequest(r)
	if err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)

		return
	}
	if err := c.crawlFormats.SaveFormats(r.Context(), settings); err != nil {
		slog.WarnContext(r.Context(), "save crawl formats failed", slog.Any("error", err))
		data := c.crawlPage(r, defaultCrawlFormFor(c.crawl))
		data.FormatsNote = "Saving format settings failed."
		c.render(r.Context(), w, c.tpl.crawl, "layout", data)

		return
	}
	http.Redirect(w, r, crawlPath, http.StatusSeeOther)
}

// crawlMonitorView returns the live crawl monitor snapshot wrapped with the
// per-request CSRF token and whether control actions are wired, or nil when no
// monitor provider exists so the Crawler section renders the start form alone.
func (c *Console) crawlMonitorView(r *http.Request) *crawlMonitorView {
	if c.monitor == nil {
		return nil
	}

	monitor := c.monitor.Monitor(r.Context())
	pagination := buildCrawlRunPagination(monitor.Runs, r.FormValue("cpage"))

	return &crawlMonitorView{
		Monitor:      monitor,
		Pagination:   pagination,
		Health:       crawlHealth(monitor),
		CSRF:         csrfToken(r),
		Controllable: c.control != nil,
		Details:      c.crawlRunDetails != nil,
	}
}

func (c *Console) handleCrawlMonitor(w http.ResponseWriter, r *http.Request) {
	if c.monitor == nil {
		http.NotFound(w, r)

		return
	}

	c.render(r.Context(), w, c.tpl.crawl, "crawl-monitor", c.crawlMonitorView(r))
}

func (c *Console) handleCrawlControl(w http.ResponseWriter, r *http.Request) {
	if c.control == nil {
		http.NotFound(w, r)

		return
	}

	req := CrawlControlRequest{
		RunID:  strings.TrimSpace(r.PostFormValue("runId")),
		Action: r.PostFormValue("action"),
	}
	if req.Action == "set_rate" {
		pagesPerMinute, err := parsePagesPerMinute(r.PostFormValue("ppm"))
		if err != nil {
			http.Error(w, "Invalid pages/minute rate.", http.StatusBadRequest)

			return
		}
		req.PagesPerMinute = pagesPerMinute
	}
	if err := c.control.Control(r.Context(), req); err != nil {
		slog.WarnContext(r.Context(), "admin crawl control failed",
			slog.String("action", req.Action), slog.Any("error", err))
	}

	// An htmx-issued control (the confirm-guarded Cancel button) swaps the refreshed
	// monitor in place; a plain form post falls back to a full-page reload.
	if r.Header.Get("HX-Request") == "true" && c.monitor != nil {
		c.render(r.Context(), w, c.tpl.crawl, "crawl-monitor", c.crawlMonitorView(r))

		return
	}

	redirectCrawlRunPage(w, requestedCrawlRunPage(r.FormValue("cpage")))
}

func parsePagesPerMinute(raw string) (uint32, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse pages per minute: %w", err)
	}

	return uint32(value), nil
}

func defaultCrawlForm() crawlForm {
	// Query URLs and TLS-authority tolerance default on: most sites paginate
	// or route through query strings, and mis-chained certificates are common
	// enough that a strict default silently empties operator crawls.
	return crawlForm{
		Mode: "url", Scope: "domain", MaxDepth: 3,
		MaxPagesPerRun:     yagocrawlcontract.DefaultMaxPagesPerRun,
		AllowQueryURLs:     true,
		IgnoreTLSAuthority: true,
		RecrawlIfOlder: yagocrawlcontract.FormatRecrawlInterval(
			yagocrawlcontract.DefaultRecrawlInterval,
		),
	}
}

func defaultCrawlFormFor(source CrawlSource) crawlForm {
	form := defaultCrawlForm()
	if defaults, ok := source.(interface{ MaxPagesPerRun() int }); ok {
		form.MaxPagesPerRun = defaults.MaxPagesPerRun()
	}

	return form
}

func parseCrawlForm(r *http.Request) crawlForm {
	depth, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("maxDepth")))
	maxPages, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("maxPagesPerHost")))
	maxPagesPerRun, _ := strconv.Atoi(strings.TrimSpace(r.PostFormValue("maxPagesPerRun")))

	return crawlForm{
		Name:                     strings.TrimSpace(r.PostFormValue("name")),
		Seeds:                    r.PostFormValue("seeds"),
		Mode:                     r.PostFormValue("mode"),
		Scope:                    r.PostFormValue("scope"),
		MaxDepth:                 depth,
		URLMustMatch:             strings.TrimSpace(r.PostFormValue("urlMustMatch")),
		URLMustNotMatch:          strings.TrimSpace(r.PostFormValue("urlMustNotMatch")),
		IndexURLMustMatch:        strings.TrimSpace(r.PostFormValue("indexMustMatch")),
		IndexURLMustNotMatch:     strings.TrimSpace(r.PostFormValue("indexMustNotMatch")),
		MaxPagesPerHost:          maxPages,
		MaxPagesPerRun:           maxPagesPerRun,
		AllowQueryURLs:           r.PostFormValue("allowQueryURLs") == "on",
		FollowNoFollowLinks:      r.PostFormValue("followNoFollowLinks") == "on",
		NoindexCanonicalMismatch: r.PostFormValue("noindexCanonicalMismatch") == "on",
		IgnoreTLSAuthority:       r.PostFormValue("ignoreTLSAuthority") == "on",
		IgnoreRobots:             r.PostFormValue("ignoreRobots") == "on",
		DisableBrowser:           r.PostFormValue("disableBrowser") == "on",
		RecrawlIfOlder:           strings.TrimSpace(r.PostFormValue("recrawlIfOlder")),
		CrawlDelay:               strings.TrimSpace(r.PostFormValue("crawlDelay")),
		ShowExpert:               r.PostFormValue("showExpert") == "on",
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
	c.renderPolicy(ctx, w, pageTemplate{tpl: tpl, name: name}, data, contentPol)
}

// pageTemplate pairs a parsed template set with the entry template it renders.
type pageTemplate struct {
	tpl  *template.Template
	name string
}

// renderPolicy renders a page under an explicit Content-Security-Policy, so a
// page that hosts the design editors can allow inline styles without relaxing
// the policy of every other console page.
func (c *Console) renderPolicy(
	ctx context.Context,
	w http.ResponseWriter,
	page pageTemplate,
	data any,
	policy string,
) {
	tpl, name := page.tpl, page.name
	writeHTMLHeadersPolicy(w, policy)
	payload := data
	// Only the full-page layout carries chrome; htmx partials render their raw
	// data so their field access stays unwrapped.
	if name == "layout" {
		payload = layoutEnvelope{
			PublicSearchHref: publicSearchHrefFromContext(ctx),
			SystemMonitor: buildSystemMonitorWithCrawler(
				c.perfHistory,
				c.crawlerFetchActivity,
			),
			Data: data,
		}
	}
	if err := tpl.ExecuteTemplate(w, name, payload); err != nil {
		slog.WarnContext(ctx, "admin console render failed",
			slog.String("template", name), slog.Any("error", err))
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, overviewPath, http.StatusFound)
}

func writeHTMLHeadersPolicy(w http.ResponseWriter, policy string) {
	header := w.Header()
	header.Set("Cache-Control", "private, no-store")
	header.Set("Content-Type", htmlType)
	header.Set("Content-Security-Policy", policy)
	header.Set("X-Content-Type-Options", "nosniff")
}

func assetHandler(assets fs.FS, catalog adminAssetCatalog) http.Handler {
	fileServer := http.StripPrefix("/admin/assets/", http.FileServerFS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, canonical := canonicalAdminAssetName(r)
		asset, found := catalog[name]
		if !canonical || !found {
			writeRejectedAdminAsset(w, r)

			return
		}
		cacheControl, accepted := adminAssetCacheControl(r.URL.RawQuery, asset.revision)
		if !accepted {
			writeRejectedAdminAsset(w, r)

			return
		}
		w.Header().Set("Cache-Control", cacheControl)
		w.Header().Set("ETag", `"`+asset.revision+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		fileServer.ServeHTTP(adminAssetResponseWriter{ResponseWriter: w}, r)
	})
}
