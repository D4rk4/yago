package compatibility

var agentAPISurfaceSpecs = []surfaceSpec{
	{
		Name:     "Tavily-compatible search",
		Path:     "/search",
		Methods:  []string{"POST"},
		State:    Partial,
		Behavior: "Serves default Tavily result fields without Yago provenance, uses local retrieval for basic, fast, and ultra-fast, uses global retrieval for advanced, and preserves the canonical root-portal order for equivalent advanced requests when click exposure randomization is disabled. Successful responses carry request IDs; errors carry only detail.error.",
		Evidence: []string{"yagonode/internal/tavilyapi/*_test.go"},
		Notes:    "Answers are deterministic extraction, local depths share one retrieval plan, and semantic reranking, geographic boosting, image ranking, real credit accounting, and external Tavily upstream mode are not implemented.",
	},
	{
		Name:     "Tavily-compatible extract",
		Path:     "/extract",
		Methods:  []string{"POST"},
		State:    Partial,
		Behavior: "Returns stored or operator-enabled egress-guarded extraction for one URL or at most 20 URLs, with reference-compatible depth, format, query, chunk, image, favicon, usage, and timeout fields under raw-scope authentication.",
		Evidence: []string{"yagonode/internal/tavilyapi/extract_endpoint_test.go"},
		Notes:    "Query chunks are bounded lexical selections, extract depth uses one extraction engine, and requested usage is local synthetic zero-credit accounting.",
	},
	{
		Name:     "Tavily-compatible crawl",
		Path:     "/crawl",
		Methods:  []string{"POST"},
		State:    Partial,
		Behavior: "Runs an authenticated egress-guarded crawl with reference-compatible fields and bounds, clipped to the node's stricter 200-page, 30-second, four-slot, and 16 MiB raw-work limits.",
		Evidence: []string{"yagonode/internal/tavilyapi/crawl_endpoint_test.go"},
		Notes:    "Instructions select bounded lexical chunks but do not guide traversal; there is no model-guided crawl or real credit accounting.",
	},
	{
		Name:     "Tavily-compatible map",
		Path:     "/map",
		Methods:  []string{"POST"},
		State:    Partial,
		Behavior: "Runs the authenticated bounded crawl walk and returns discovered URLs without page content, using reference-compatible fields and the node's stricter raw-work limits.",
		Evidence: []string{"yagonode/internal/tavilyapi/crawl_endpoint_test.go"},
		Notes:    "Instructions are accepted but do not alter discovery; there is no model-guided mapping or real credit accounting.",
	},
}
