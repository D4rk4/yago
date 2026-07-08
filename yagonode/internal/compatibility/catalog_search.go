package compatibility

import "github.com/D4rk4/yago/yagoproto"

var searchSurfaceSpecs = []surfaceSpec{
	{
		Name:    "YaCy search JSON",
		Path:    yagoproto.PathYaCySearchJSON,
		Methods: []string{"GET"},
		State:   Partial,
		Behavior: "Serves an upstream-like JSON search response backed by local full-text search " +
			"and DHT-selected reachable-peer search with YaCy indexabstract negotiation for multi-term remote searches.",
		Evidence: []string{
			"yagonode/internal/yacysearch/*_test.go",
			"yagonode/internal/searchlocal/*_test.go",
			"yagonode/internal/searchindex/*_test.go",
			"yagonode/internal/searchremote/*_test.go",
		},
		Notes: "HTML parity, richer navigation, and persistent suggestions remain incomplete.",
	},
	{
		Name:     "YaCy search RSS",
		Path:     yagoproto.PathYaCySearchRSS,
		Methods:  []string{"GET"},
		State:    Partial,
		Behavior: "Serves OpenSearch-flavored RSS search responses backed by the same local full-text and federated search backend as JSON search.",
		Evidence: []string{
			"yagonode/internal/yacysearch/*_test.go",
			"yagonode/internal/searchlocal/*_test.go",
		},
		Notes: "HTML parity and richer YaCy RSS fields remain incomplete.",
	},
	{
		Name:     "YaCy search HTML",
		Path:     yagoproto.PathYaCySearchHTML,
		Methods:  []string{"GET"},
		State:    Partial,
		Behavior: "Serves a simple YaCy-compatible public search form and result list backed by the local full-text and federated search backend.",
		Evidence: []string{
			"yagonode/internal/yacysearch/*_test.go",
			"yagonode/internal/searchlocal/*_test.go",
		},
		Notes: "Full Java YaCy page parity remains incomplete.",
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
		Name:     "JSON suggestions",
		Path:     yagoproto.PathSuggestJSON,
		Methods:  []string{"GET"},
		State:    Partial,
		Behavior: "Serves OpenSearch suggestion JSON from bounded in-memory recent queries.",
		Evidence: []string{"yagonode/internal/yacysearch/*_test.go"},
		Notes:    "Suggestions are not persisted.",
	},
	{
		Name:     "XML suggestions",
		Path:     yagoproto.PathSuggestXML,
		Methods:  []string{"GET"},
		State:    Partial,
		Behavior: "Serves YaCy-compatible SearchSuggestion XML from bounded in-memory recent queries.",
		Evidence: []string{"yagonode/internal/yacysearch/*_test.go"},
		Notes:    "Suggestions are not persisted.",
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
}
