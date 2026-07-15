package searchindex

func storedProximityEligible(results []SearchResult, req SearchRequest) bool {
	if !req.IncludePositions {
		return false
	}
	if len(distinctWords(queryTermWords(req))) >= 2 {
		return true
	}
	for _, result := range results {
		if result.Proximity > 0 || result.OrderedProximity > 0 {
			return true
		}
	}

	return false
}
