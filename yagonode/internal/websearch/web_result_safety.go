package websearch

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

func webResultSafetyRating(result Result) searchcore.SafetyRating {
	if result.AdultContentFiltered {
		return searchcore.SafetyProviderFiltered
	}

	return searchcore.SafetyUnknown
}
