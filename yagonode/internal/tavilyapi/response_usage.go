package tavilyapi

func responseUsageEnabled(enabled bool) *SearchUsage {
	if !enabled {
		return nil
	}

	return &SearchUsage{Credits: 0}
}
