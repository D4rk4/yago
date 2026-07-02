package compatibility

var agentAPISurfaceSpecs = []surfaceSpec{
	{
		Name:     "Tavily-compatible search",
		Path:     "/search",
		Methods:  []string{"POST"},
		State:    Planned,
		Behavior: "No Tavily-compatible local/P2P search endpoint is mounted yet.",
		Notes:    "External Tavily upstream mode is also not implemented.",
	},
	{
		Name:     "Tavily-compatible extract",
		Path:     "/extract",
		Methods:  []string{"POST"},
		State:    Planned,
		Behavior: "No Tavily-compatible extract endpoint is mounted yet.",
	},
}
