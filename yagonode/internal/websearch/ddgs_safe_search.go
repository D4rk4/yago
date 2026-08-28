package websearch

import (
	"errors"
	"strings"
)

const safeSearchStrict = "strict"

var errStrictSafeSearchUnavailable = errors.New(
	"selected web-search backend cannot enforce strict safe search",
)

func effectiveSafeSearchMode(requested string, configured string) string {
	if strings.EqualFold(strings.TrimSpace(requested), safeSearchStrict) {
		return safeSearchStrict
	}

	return strings.ToLower(strings.TrimSpace(configured))
}

func hasStrictSafeSearchEngine(engines []engine) bool {
	for _, backend := range engines {
		if backend.strictSafeSearch {
			return true
		}
	}

	return false
}

func enginePermitsSafeSearch(backend engine, mode string) bool {
	return mode != safeSearchStrict || backend.strictSafeSearch
}

func markAdultContentFiltered(results []Result, backend engine, mode string) []Result {
	if !enginePermitsSafeSearch(backend, mode) || mode != safeSearchStrict {
		return results
	}
	for index := range results {
		results[index].AdultContentFiltered = true
	}

	return results
}
