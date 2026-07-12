package searchindex

import "strings"

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
		cloned[index] = FacetGroup{
			Name:  strings.Clone(group.Name),
			Terms: cloneFacetTerms(group.Terms),
		}
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
		cloneSearchResultStrings(&cloned[index])
		cloned[index].FieldScores = cloneFieldScores(result.FieldScores)
		cloned[index].FieldTermPositions = cloneFieldTermPositions(result.FieldTermPositions)
		cloned[index].Images = cloneResultImages(result.Images)
	}

	return cloned
}

func cloneFieldScores(scores map[string]float64) map[string]float64 {
	if scores == nil {
		return nil
	}
	cloned := make(map[string]float64, len(scores))
	for field, score := range scores {
		cloned[strings.Clone(field)] = score
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
		cloned[strings.Clone(field)] = cloneTermPositions(terms)
	}

	return cloned
}

func cloneTermPositions(positions map[string][]int) map[string][]int {
	if positions == nil {
		return nil
	}
	cloned := make(map[string][]int, len(positions))
	for term, values := range positions {
		cloned[strings.Clone(term)] = cloneValues(values)
	}

	return cloned
}

func cloneSearchResultStrings(result *SearchResult) {
	result.DocumentID = strings.Clone(result.DocumentID)
	result.ClusterID = strings.Clone(result.ClusterID)
	result.RepresentativeURL = strings.Clone(result.RepresentativeURL)
	result.Title = strings.Clone(result.Title)
	result.URL = strings.Clone(result.URL)
	result.Snippet = strings.Clone(result.Snippet)
	result.RawContent = strings.Clone(result.RawContent)
	result.Explanation = strings.Clone(result.Explanation)
	result.Author = strings.Clone(result.Author)
	result.Keywords = strings.Clone(result.Keywords)
	result.Publisher = strings.Clone(result.Publisher)
	result.Language = strings.Clone(result.Language)
	result.Analyzer = strings.Clone(result.Analyzer)
	result.ContentType = strings.Clone(result.ContentType)
}

func cloneFacetTerms(terms []FacetTerm) []FacetTerm {
	if terms == nil {
		return nil
	}
	cloned := make([]FacetTerm, len(terms))
	for index, term := range terms {
		cloned[index] = FacetTerm{Term: strings.Clone(term.Term), Count: term.Count}
	}

	return cloned
}

func cloneResultImages(images []ResultImage) []ResultImage {
	if images == nil {
		return nil
	}
	cloned := make([]ResultImage, len(images))
	for index, image := range images {
		cloned[index] = ResultImage{
			URL: strings.Clone(image.URL),
			Alt: strings.Clone(image.Alt),
		}
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
