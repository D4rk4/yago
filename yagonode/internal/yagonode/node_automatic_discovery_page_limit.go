package yagonode

func automaticDiscoveryPageLimit(configured, crawlerMaximum int) int {
	if crawlerMaximum > 0 && crawlerMaximum < configured {
		return crawlerMaximum
	}

	return configured
}
