package searchsession

import (
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func cloneSessionResults(results []searchcore.Result) []searchcore.Result {
	if results == nil {
		return nil
	}
	cloned := make([]searchcore.Result, len(results))
	for index, result := range results {
		cloned[index] = cloneSessionResult(result)
	}

	return cloned
}

func cloneSessionResult(result searchcore.Result) searchcore.Result {
	result.DocumentID = strings.Clone(result.DocumentID)
	result.Analyzer = strings.Clone(result.Analyzer)
	result.Title = strings.Clone(result.Title)
	result.URL = strings.Clone(result.URL)
	result.ClusterID = strings.Clone(result.ClusterID)
	result.RepresentativeURL = strings.Clone(result.RepresentativeURL)
	result.DisplayURL = strings.Clone(result.DisplayURL)
	result.Snippet = strings.Clone(result.Snippet)
	result.Source = searchcore.Source(strings.Clone(string(result.Source)))
	result.Host = strings.Clone(result.Host)
	result.Path = strings.Clone(result.Path)
	result.File = strings.Clone(result.File)
	result.ContentType = strings.Clone(result.ContentType)
	result.URLHash = strings.Clone(result.URLHash)
	result.Date = strings.Clone(result.Date)
	result.ContentDomain = searchcore.ContentDomain(strings.Clone(string(result.ContentDomain)))
	result.Language = strings.Clone(result.Language)
	result.Author = strings.Clone(result.Author)
	result.Keywords = strings.Clone(result.Keywords)
	result.Publisher = strings.Clone(result.Publisher)
	result.Explanation = strings.Clone(result.Explanation)
	result.Images = cloneSessionImages(result.Images)
	result.QueryMatches = cloneSessionQueryMatches(result.QueryMatches)
	result.FieldScores = cloneSessionScores(result.FieldScores)
	result.FieldTermPositions = cloneSessionPositions(result.FieldTermPositions)

	return result
}

func cloneSessionQueryMatches(matches []searchcore.QueryMatch) []searchcore.QueryMatch {
	if matches == nil {
		return nil
	}
	cloned := make([]searchcore.QueryMatch, len(matches))
	copy(cloned, matches)

	return cloned
}

func cloneSessionImages(images []searchcore.ResultImage) []searchcore.ResultImage {
	if images == nil {
		return nil
	}
	cloned := make([]searchcore.ResultImage, len(images))
	for index, image := range images {
		cloned[index] = searchcore.ResultImage{
			URL: strings.Clone(image.URL),
			Alt: strings.Clone(image.Alt),
		}
	}

	return cloned
}

func cloneSessionScores(scores map[string]float64) map[string]float64 {
	if scores == nil {
		return nil
	}
	cloned := make(map[string]float64, len(scores))
	for field, score := range scores {
		cloned[strings.Clone(field)] = score
	}

	return cloned
}

func cloneSessionPositions(
	fields map[string]map[string][]int,
) map[string]map[string][]int {
	if fields == nil {
		return nil
	}
	cloned := make(map[string]map[string][]int, len(fields))
	for field, terms := range fields {
		cloned[strings.Clone(field)] = cloneSessionTermPositions(terms)
	}

	return cloned
}

func cloneSessionTermPositions(terms map[string][]int) map[string][]int {
	if terms == nil {
		return nil
	}
	cloned := make(map[string][]int, len(terms))
	for term, positions := range terms {
		cloned[strings.Clone(term)] = cloneSessionPositionValues(positions)
	}

	return cloned
}

func cloneSessionPositionValues(positions []int) []int {
	if positions == nil {
		return nil
	}
	cloned := make([]int, len(positions))
	copy(cloned, positions)

	return cloned
}

func cloneSessionFailures(
	failures []searchcore.PartialFailure,
) []searchcore.PartialFailure {
	if failures == nil {
		return nil
	}
	cloned := make([]searchcore.PartialFailure, len(failures))
	for index, failure := range failures {
		cloned[index] = searchcore.PartialFailure{
			Source: strings.Clone(failure.Source),
			Reason: strings.Clone(failure.Reason),
		}
	}

	return cloned
}

func cloneSessionFacets(facets []searchcore.FacetGroup) []searchcore.FacetGroup {
	if facets == nil {
		return nil
	}
	cloned := make([]searchcore.FacetGroup, len(facets))
	for index, facet := range facets {
		cloned[index] = searchcore.FacetGroup{
			Name:  strings.Clone(facet.Name),
			Terms: cloneSessionFacetTerms(facet.Terms),
		}
	}

	return cloned
}

func cloneSessionFacetTerms(terms []searchcore.FacetTerm) []searchcore.FacetTerm {
	if terms == nil {
		return nil
	}
	cloned := make([]searchcore.FacetTerm, len(terms))
	for index, term := range terms {
		cloned[index] = searchcore.FacetTerm{
			Term:  strings.Clone(term.Term),
			Count: term.Count,
		}
	}

	return cloned
}
