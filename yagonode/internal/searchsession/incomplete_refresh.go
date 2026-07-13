package searchsession

import "github.com/D4rk4/yago/yagonode/internal/searchcore"

func incompleteRefresh(response searchcore.Response) bool {
	if hasPrimaryResults(response.Results) {
		return false
	}
	for _, failure := range response.PartialFailures {
		switch failure.Source {
		case searchcore.PartialFailureSourceExactStage,
			searchcore.PartialFailureSourceLocalExactStage,
			searchcore.PartialFailureSourceFuzzyStage,
			searchcore.PartialFailureSourceLocalSearch,
			searchcore.PartialFailureSourceRemoteStage,
			searchcore.PartialFailureSourceWeb:
			return true
		}
	}

	return false
}

func hasPrimaryResults(results []searchcore.Result) bool {
	for _, result := range results {
		if !result.FromWeb() {
			return true
		}
	}

	return false
}

func responseWithRefreshFailures(
	response searchcore.Response,
	failures []searchcore.PartialFailure,
) searchcore.Response {
	response.PartialFailures = mergedSessionFailures(response.PartialFailures, failures)

	return response
}

func mergedSessionFailures(
	current []searchcore.PartialFailure,
	additional []searchcore.PartialFailure,
) []searchcore.PartialFailure {
	merged := cloneSessionFailures(current)
	for _, candidate := range additional {
		found := false
		for _, existing := range merged {
			if existing == candidate {
				found = true

				break
			}
		}
		if !found {
			merged = append(merged, candidate)
		}
	}

	return merged
}
