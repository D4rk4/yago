package compatibility

var agentAPISurfaceSpecs = []surfaceSpec{
	{
		Name:     "Tavily-compatible search",
		Path:     "/search",
		Methods:  []string{"POST"},
		State:    Partial,
		Behavior: "Serves default Tavily result fields without Yago provenance, uses local retrieval for basic, fast, and ultra-fast, uses global retrieval for advanced, and preserves the canonical root-portal order for equivalent advanced requests when click exposure randomization is disabled. Successful responses carry request IDs; errors carry only detail.error. Requested usage reports one request-local compatible unit for an executed basic, fast, or ultra-fast search and two for an executed advanced search.",
		Evidence: []string{"yagonode/internal/tavilyapi/*_test.go"},
		Notes:    "Answers are deterministic extraction, local depths share one retrieval plan, and semantic reranking, geographic boosting, image ranking, and external Tavily upstream mode are not implemented. Usage is request-local compatibility accounting, not billing, an account balance, external-provider spend, or evidence of an upstream Tavily call.",
	},
	{
		Name:     "Tavily-compatible extract",
		Path:     "/extract",
		Methods:  []string{"POST"},
		State:    Partial,
		Behavior: "Returns stored or operator-enabled egress-guarded extraction for one URL or at most 20 URLs, with reference-compatible depth, format, query, chunk, image, favicon, usage, and timeout fields under raw-scope authentication. Requested usage reports request-local compatible units from successful extractions in complete groups of five, doubled for advanced depth.",
		Evidence: []string{"yagonode/internal/tavilyapi/extract_endpoint_test.go"},
		Notes:    "Query chunks are bounded lexical selections and extract depth uses one extraction engine. Usage is request-local compatibility accounting rather than billing or external-provider spend; failed extractions do not contribute.",
	},
	{
		Name:     "Tavily-compatible crawl",
		Path:     "/crawl",
		Methods:  []string{"POST"},
		State:    Partial,
		Behavior: "Runs an authenticated egress-guarded crawl with reference-compatible fields and bounds, clipped to the node's stricter 200-page, 30-second, four-slot, and 16 MiB raw-work limits. Requested usage is the request-local sum of mapping units from complete groups of ten successful pages and extraction units from complete groups of five successful pages.",
		Evidence: []string{"yagonode/internal/tavilyapi/crawl_endpoint_test.go"},
		Notes:    "Instructions select bounded lexical chunks but do not guide traversal, and there is no model-guided crawl. Mapping units double when instructions are present; extraction units double for advanced depth. Usage is not billing or external-provider spend.",
	},
	{
		Name:     "Tavily-compatible map",
		Path:     "/map",
		Methods:  []string{"POST"},
		State:    Partial,
		Behavior: "Runs the authenticated bounded crawl walk and returns discovered URLs without page content, using reference-compatible fields and the node's stricter raw-work limits. Requested usage reports request-local mapping units from complete groups of ten successful pages.",
		Evidence: []string{"yagonode/internal/tavilyapi/crawl_endpoint_test.go"},
		Notes:    "Instructions are accepted but do not alter discovery, and there is no model-guided mapping. Mapping usage doubles when instructions are present; usage is not billing or external-provider spend.",
	},
}
