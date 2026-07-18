package searchindex

import (
	"container/list"
	"reflect"
)

const (
	retainedSearchMapBytes      = 256
	retainedSearchMapEntryBytes = 64
)

var (
	retainedSearchMaximumInt       = int(^uint(0) >> 1)
	retainedSearchEntryWidth       = reflect.TypeOf(cachedSearchEntry{}).Size()
	retainedSearchListElementWidth = reflect.TypeOf(list.Element{}).Size()
	retainedSearchResultWidth      = reflect.TypeOf(SearchResult{}).Size()
	retainedSearchImageWidth       = reflect.TypeOf(ResultImage{}).Size()
	retainedSearchFacetGroupWidth  = reflect.TypeOf(FacetGroup{}).Size()
	retainedSearchFacetTermWidth   = reflect.TypeOf(FacetTerm{}).Size()
	retainedSearchPositionWidth    = reflect.TypeOf(int(0)).Size()
	retainedSearchQueryMatchWidth  = reflect.TypeOf(TextQueryMatch{}).Size()
)

func retainedSearchEntryBytes(entry *cachedSearchEntry) int {
	retained := retainedSearchProduct(1, retainedSearchEntryWidth)
	retained = retainedSearchAdd(
		retained,
		retainedSearchProduct(1, retainedSearchListElementWidth),
	)
	retained = retainedSearchStrings(retained, entry.key)
	retained = retainedSearchAdd(
		retained,
		retainedSearchProduct(cap(entry.results.Results), retainedSearchResultWidth),
	)
	for _, result := range entry.results.Results {
		retained = retainedSearchResultBytes(retained, result)
	}
	retained = retainedSearchAdd(
		retained,
		retainedSearchProduct(cap(entry.results.Facets), retainedSearchFacetGroupWidth),
	)
	for _, facet := range entry.results.Facets {
		retained = retainedSearchStrings(retained, facet.Name)
		retained = retainedSearchAdd(
			retained,
			retainedSearchProduct(cap(facet.Terms), retainedSearchFacetTermWidth),
		)
		for _, term := range facet.Terms {
			retained = retainedSearchStrings(retained, term.Term)
		}
	}

	return retained
}

func retainedSearchResultBytes(retained int, result SearchResult) int {
	retained = retainedSearchStrings(
		retained,
		result.DocumentID,
		result.ClusterID,
		result.RepresentativeURL,
		result.Title,
		result.URL,
		result.Snippet,
		result.RawContent,
		result.Explanation,
		result.Author,
		result.Keywords,
		result.Publisher,
		result.Language,
		result.Analyzer,
		result.ContentType,
	)
	retained = retainedSearchAdd(
		retained,
		retainedSearchProduct(cap(result.Images), retainedSearchImageWidth),
	)
	retained = retainedSearchAdd(
		retained,
		retainedSearchProduct(
			cap(result.BodyQueryMatches),
			retainedSearchQueryMatchWidth,
		),
	)
	for _, image := range result.Images {
		retained = retainedSearchStrings(retained, image.URL, image.Alt)
	}
	retained = retainedSearchScores(retained, result.FieldScores)
	retained = retainedSearchPositions(retained, result.FieldTermPositions)

	return retained
}

func retainedSearchScores(retained int, scores map[string]float64) int {
	if scores == nil {
		return retained
	}
	retained = retainedSearchAdd(retained, retainedSearchMapBytes)
	for field := range scores {
		retained = retainedSearchAdd(retained, retainedSearchMapEntryBytes)
		retained = retainedSearchStrings(retained, field)
	}

	return retained
}

func retainedSearchPositions(
	retained int,
	fields map[string]map[string][]int,
) int {
	if fields == nil {
		return retained
	}
	retained = retainedSearchAdd(retained, retainedSearchMapBytes)
	for field, terms := range fields {
		retained = retainedSearchAdd(retained, retainedSearchMapEntryBytes)
		retained = retainedSearchStrings(retained, field)
		retained = retainedSearchAdd(retained, retainedSearchMapBytes)
		for term, positions := range terms {
			retained = retainedSearchAdd(retained, retainedSearchMapEntryBytes)
			retained = retainedSearchStrings(retained, term)
			retained = retainedSearchAdd(
				retained,
				retainedSearchProduct(cap(positions), retainedSearchPositionWidth),
			)
		}
	}

	return retained
}

func retainedSearchStrings(retained int, values ...string) int {
	for _, value := range values {
		retained = retainedSearchAdd(retained, len(value))
	}

	return retained
}

func retainedSearchProduct(length int, width uintptr) int {
	if width != 0 && uintptr(length) > uintptr(retainedSearchMaximumInt)/width {
		return retainedSearchMaximumInt
	}

	return length * int(width)
}

func retainedSearchAdd(retained int, added int) int {
	if added > retainedSearchMaximumInt-retained {
		return retainedSearchMaximumInt
	}

	return retained + added
}
