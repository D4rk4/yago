package compatibility

import "github.com/D4rk4/yago/yacyproto"

var searchSurfaceSpecs = []surfaceSpec{
	{
		Name:    "YaCy search JSON",
		Path:    yacyproto.PathYaCySearchJSON,
		Methods: []string{"GET"},
		State:   Partial,
		Behavior: "Serves an upstream-like JSON search response backed by local full-text search " +
			"and DHT-selected reachable-peer search with YaCy indexabstract negotiation for multi-term remote searches.",
		Evidence: []string{
			"yacynode/internal/yacysearch/*_test.go",
			"yacynode/internal/searchlocal/*_test.go",
			"yacynode/internal/searchindex/*_test.go",
			"yacynode/internal/searchremote/*_test.go",
		},
		Notes: "HTML parity, richer navigation, and persistent suggestions remain incomplete.",
	},
	{
		Name:     "YaCy search RSS",
		Path:     yacyproto.PathYaCySearchRSS,
		Methods:  []string{"GET"},
		State:    Partial,
		Behavior: "Serves OpenSearch-flavored RSS search responses backed by the same local full-text and federated search backend as JSON search.",
		Evidence: []string{
			"yacynode/internal/yacysearch/*_test.go",
			"yacynode/internal/searchlocal/*_test.go",
		},
		Notes: "HTML parity and richer YaCy RSS fields remain incomplete.",
	},
	{
		Name:     "YaCy search HTML",
		Path:     yacyproto.PathYaCySearchHTML,
		Methods:  []string{"GET"},
		State:    Partial,
		Behavior: "Serves a simple YaCy-compatible public search form and result list backed by the local full-text and federated search backend.",
		Evidence: []string{
			"yacynode/internal/yacysearch/*_test.go",
			"yacynode/internal/searchlocal/*_test.go",
		},
		Notes: "Full Java YaCy page parity remains incomplete.",
	},
	{
		Name:     "OpenSearch description",
		Path:     yacyproto.PathOpenSearch,
		Methods:  []string{"GET"},
		State:    Implemented,
		Behavior: "Advertises HTML, RSS, JSON suggestion, and XML suggestion URLs for the current public base URL.",
		Evidence: []string{"yacynode/internal/yacysearch/*_test.go"},
	},
	{
		Name:     "JSON suggestions",
		Path:     yacyproto.PathSuggestJSON,
		Methods:  []string{"GET"},
		State:    Partial,
		Behavior: "Serves OpenSearch suggestion JSON from bounded in-memory recent queries.",
		Evidence: []string{"yacynode/internal/yacysearch/*_test.go"},
		Notes:    "Suggestions are not persisted.",
	},
	{
		Name:     "XML suggestions",
		Path:     yacyproto.PathSuggestXML,
		Methods:  []string{"GET"},
		State:    Partial,
		Behavior: "Serves YaCy-compatible SearchSuggestion XML from bounded in-memory recent queries.",
		Evidence: []string{"yacynode/internal/yacysearch/*_test.go"},
		Notes:    "Suggestions are not persisted.",
	},
	{
		Name:     "Solr select compatibility",
		Path:     "/solr/select",
		Methods:  []string{"GET", "POST"},
		State:    Planned,
		Behavior: "No Solr-compatible endpoint is mounted yet.",
		Notes:    "Do not claim full Solr compatibility.",
	},
	{
		Name:     "Full embedded Solr API",
		Path:     "/solr/*",
		Methods:  []string{"GET", "POST"},
		State:    Unsupported,
		Behavior: "Full embedded Solr server compatibility is not a Go peer target.",
		Notes:    "Only a bounded /solr/select subset is planned.",
	},
	{
		Name:     "GSA search compatibility",
		Path:     "/gsa/searchresult",
		Methods:  []string{"GET"},
		State:    Planned,
		Behavior: "No Google Search Appliance compatibility endpoint is mounted yet.",
	},
}
