package compatibility

var agentAPISurfaceSpecs = []surfaceSpec{
	{
		Name:     "Tavily-compatible search",
		Path:     "/search",
		Methods:  []string{"POST"},
		State:    Partial,
		Behavior: "Serves a Tavily-like search response over the shared search core, accepts current search contract fields, optionally requires local bearer auth when YAGO_SEARCH_API_KEY is set, returns request IDs and JSON error envelopes, uses local search for basic, fast, and ultra-fast depths, and includes DHT-selected peer search when search_depth is advanced.",
		Evidence: []string{"yacynode/internal/tavilyapi/*_test.go"},
		Notes:    "Local answer generation, image search, real usage accounting, hashed API key storage, scopes, rate limits, and external Tavily upstream mode are not implemented.",
	},
	{
		Name:     "Tavily-compatible extract",
		Path:     "/extract",
		Methods:  []string{"POST"},
		State:    Planned,
		Behavior: "No Tavily-compatible extract endpoint is mounted yet.",
	},
}
