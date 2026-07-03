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
		State:    Partial,
		Behavior: "Returns Tavily-like extract results for URLs already in the document store, accepts urls as a string or array plus extract_depth, format, include_images, and include_favicon, optionally requires local bearer auth when YAGO_SEARCH_API_KEY is set, and returns request IDs and JSON error envelopes. Fetch-on-extract is disabled, so URLs absent from the store return controlled failed_results entries and no private-network fetch occurs.",
		Evidence: []string{"yacynode/internal/tavilyapi/extract_endpoint_test.go"},
		Notes:    "Fetch-on-extract for uncached URLs, markdown fidelity beyond a title heading, and image ranking remain planned.",
	},
}
