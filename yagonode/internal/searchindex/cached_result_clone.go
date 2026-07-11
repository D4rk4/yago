package searchindex

func cloneResultSet(set SearchResultSet) SearchResultSet {
	return SearchResultSet{
		Facets:  cloneFacetGroups(set.Facets),
		Results: cloneSearchResults(set.Results),
		Total:   set.Total,
	}
}

func cloneFacetGroups(groups []FacetGroup) []FacetGroup {
	if groups == nil {
		return nil
	}
	cloned := make([]FacetGroup, len(groups))
	for index, group := range groups {
		cloned[index] = group
		cloned[index].Terms = cloneValues(group.Terms)
	}

	return cloned
}

func cloneSearchResults(results []SearchResult) []SearchResult {
	if results == nil {
		return nil
	}
	cloned := make([]SearchResult, len(results))
	for index, result := range results {
		cloned[index] = result
		cloned[index].FieldScores = cloneFieldScores(result.FieldScores)
		cloned[index].FieldTermPositions = cloneFieldTermPositions(result.FieldTermPositions)
		cloned[index].Images = cloneValues(result.Images)
	}

	return cloned
}

func cloneFieldScores(scores map[string]float64) map[string]float64 {
	if scores == nil {
		return nil
	}
	cloned := make(map[string]float64, len(scores))
	for field, score := range scores {
		cloned[field] = score
	}

	return cloned
}

func cloneFieldTermPositions(
	positions map[string]map[string][]int,
) map[string]map[string][]int {
	if positions == nil {
		return nil
	}
	cloned := make(map[string]map[string][]int, len(positions))
	for field, terms := range positions {
		cloned[field] = cloneTermPositions(terms)
	}

	return cloned
}

func cloneTermPositions(positions map[string][]int) map[string][]int {
	if positions == nil {
		return nil
	}
	cloned := make(map[string][]int, len(positions))
	for term, values := range positions {
		cloned[term] = cloneValues(values)
	}

	return cloned
}

func cloneValues[T any](values []T) []T {
	if values == nil {
		return nil
	}
	cloned := make([]T, len(values))
	copy(cloned, values)

	return cloned
}
