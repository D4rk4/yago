package yagonode

import "time"

var webFallbackParallelExactStageBudget = 1400 * time.Millisecond

func webFallbackPrimaryStageBudget(config webFallbackConfig) time.Duration {
	if effectiveWebFallbackPrivacy(config) == webFallbackPrivacyAlways {
		return webFallbackParallelExactStageBudget
	}

	return webFallbackExactStageBudget
}
