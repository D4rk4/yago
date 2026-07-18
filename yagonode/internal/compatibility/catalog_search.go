package compatibility

import "github.com/D4rk4/yago/yagoproto"

var searchSurfaceSpecs = []surfaceSpec{
	{
		Name:    "YaCy search JSON",
		Path:    yagoproto.PathYaCySearchJSON,
		Methods: []string{"GET"},
		State:   Implemented,
		Behavior: "Serves an upstream-like JSON search response backed by local full-text search " +
			"and DHT-selected reachable-peer search with YaCy indexabstract negotiation for multi-term remote searches, " +
			"with the channel image/opensearch fields, the full item shape " +
			"(title/link/code/description/pubDate/size/sizename/guid/host/path/file/urlhash/ranking), and, on a nav= " +
			"request, the navigation array (hosts/authors/filetypes/languages/protocols/dates with counts and refine " +
			"modifier/url).",
		Evidence: []string{
			"yagonode/internal/yacysearch/*_test.go",
			"yagonode/internal/searchlocal/*_test.go",
			"yagonode/internal/searchindex/*_test.go",
			"yagonode/internal/searchremote/*_test.go",
		},
		Notes: "Query suggestions are served by the dedicated /suggest.json and /suggest.xml endpoints rather than " +
			"embedded in the search response, matching upstream; the YaCy-internal faviconCode favicon-hash is omitted " +
			"(this node proxies favicons by host URL instead).",
	},
	{
		Name:    "YaCy search RSS",
		Path:    yagoproto.PathYaCySearchRSS,
		Methods: []string{"GET"},
		State:   Implemented,
		Behavior: "Serves OpenSearch-flavored RSS search responses backed by the same local full-text and federated " +
			"search backend as JSON search, with per-item Dublin Core dc:creator/dc:publisher/dc:subject from extracted " +
			"document metadata, the yacy:size/sizename/host/path/file fields, and, on a nav= request, the " +
			"yacy:navigation facet element (same navigators, counts, and refine modifiers as the JSON surface).",
		Evidence: []string{
			"yagonode/internal/yacysearch/*_test.go",
			"yagonode/internal/searchlocal/*_test.go",
		},
		Notes: "Image-vertical media enclosures reuse the shared text-first result layout rather than YaCy's " +
			"per-contentdom media: elements — the same text-first-node simplification as the HTML surface, not a wire gap.",
	},
	{
		Name:    "YaCy search HTML",
		Path:    yagoproto.PathYaCySearchHTML,
		Methods: []string{"GET"},
		State:   Implemented,
		Behavior: "Serves a YaCy-compatible public search form and result list backed by the local full-text and " +
			"federated search backend, with filter-preserving numbered and prev/next pagination and, on a nav= request, " +
			"collapsible navigator refine links (provider, filetype, language, author, protocol, date).",
		Evidence: []string{
			"yagonode/internal/yacysearch/*_test.go",
			"yagonode/internal/searchlocal/*_test.go",
		},
		Notes: "Result rows use one shared text layout across content domains rather than YaCy's per-contentdom " +
			"thumbnail views — a deliberate text-first-node simplification, not a wire-protocol gap.",
	},
	{
		Name:     "OpenSearch description",
		Path:     yagoproto.PathOpenSearch,
		Methods:  []string{"GET"},
		State:    Implemented,
		Behavior: "Advertises HTML, RSS, JSON suggestion, and XML suggestion URLs for the current public base URL.",
		Evidence: []string{"yagonode/internal/yacysearch/*_test.go"},
	},
	{
		Name:    "JSON suggestions",
		Path:    yagoproto.PathSuggestJSON,
		Methods: []string{"GET"},
		State:   Implemented,
		Behavior: "Serves the OpenSearch suggestion array from live-index document titles merged with recent queries, " +
			"honouring count (clamped to 30), timeout (default 300ms), a validated JSONP callback, and open CORS.",
		Evidence: []string{"yagonode/internal/yacysearch/*_test.go"},
		Notes:    "Wire-identical source difference from upstream's term-dictionary DidYouMean; real indexed titles instead.",
	},
	{
		Name:    "XML suggestions",
		Path:    yagoproto.PathSuggestXML,
		Methods: []string{"GET"},
		State:   Implemented,
		Behavior: "Serves YaCy-compatible SearchSuggestion XML from the same index-title and recent-query source, " +
			"honouring count/timeout and setting the open CORS header upstream sends.",
		Evidence: []string{"yagonode/internal/yacysearch/*_test.go"},
	},
	{
		Name:     "Solr select compatibility",
		Path:     "/solr/select",
		Methods:  []string{"GET", "POST"},
		State:    Unsupported,
		Behavior: "No Solr-compatible endpoint is mounted; Solr query compatibility is dropped.",
		Notes:    "Local full-text search uses the native Go backend instead; see ADR 0012.",
	},
	{
		Name:     "Full embedded Solr API",
		Path:     "/solr/*",
		Methods:  []string{"GET", "POST"},
		State:    Unsupported,
		Behavior: "Full embedded Solr server compatibility is not a Go peer target.",
		Notes:    "No Solr subset is planned; the native Go search backend replaces it.",
	},
	{
		Name:    "GSA search compatibility",
		Path:    "/gsa/searchresult",
		Methods: []string{"GET"},
		State:   Unsupported,
		Behavior: "Not mounted, and no longer a target: upstream YaCy removed GSA support in 2020, " +
			"so there is no live surface to be compatible with.",
	},
	{
		Name:  "MCP and OpenAI-compatible AI surfaces",
		Path:  "/tools*, /v1/*, /api/tags",
		State: Unsupported,
		Behavior: "Deliberate non-goal (operator decision, 2026-07): upstream YaCy grew an MCP " +
			"JSON-RPC search server and OpenAI/Ollama proxy endpoints, but this node's agent " +
			"surface is the Tavily-compatible /search, /extract, /crawl, and /map API, one agent protocol kept simple.",
	},
}
