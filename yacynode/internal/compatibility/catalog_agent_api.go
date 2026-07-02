package compatibility

var agentAPISurfaceSpecs = []surfaceSpec{
	{
		Name:     "Tavily-compatible search",
		Path:     "/search",
		Methods:  []string{"POST"},
		State:    Partial,
		Behavior: "Serves a Tavily-like search response over the local search core, with basic local search by default and DHT-selected peer search when search_depth is advanced.",
		Evidence: []string{"yacynode/internal/tavilyapi/*_test.go"},
		Notes:    "Local answer generation, images, usage accounting, and external Tavily upstream mode are not implemented.",
	},
	{
		Name:     "Tavily-compatible extract",
		Path:     "/extract",
		Methods:  []string{"POST"},
		State:    Planned,
		Behavior: "No Tavily-compatible extract endpoint is mounted yet.",
	},
}
