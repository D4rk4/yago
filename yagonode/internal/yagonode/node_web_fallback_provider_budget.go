package yagonode

import "time"

func webFallbackProviderStageBudget(config webFallbackConfig) time.Duration {
	if effectiveWebFallbackPrivacy(config) == webFallbackPrivacyAlways {
		return webFallbackParallelProviderBudget
	}

	return webFallbackProviderBudget
}
