package searchindex

import "github.com/blevesearch/bleve/v2/search"

func newStoredFieldEvidence(matcher *storedEvidenceMatcher) storedFieldEvidence {
	requirementTerms := make(search.TermLocationMap, len(matcher.rawRequirements))
	exactTerms := make(search.TermLocationMap, len(matcher.rawRequirements))
	for _, requirement := range matcher.rawRequirements {
		requirementTerms[requirement] = nil
		exactTerms[requirement] = nil
	}

	return storedFieldEvidence{
		terms:            search.TermLocationMap{},
		requirementTerms: requirementTerms,
		targetTerms:      make(map[int]search.Locations, len(matcher.targets)),
		exactTerms:       exactTerms,
		phraseTerms:      search.TermLocationMap{},
		bestSpan:         int(^uint(0) >> 1),
		queryTerms:       matcher.queries,
	}
}

func (m *storedEvidenceMatcher) relaxedPassageMinimum(req SearchRequest) int {
	if req.Relaxed {
		return relaxedRequirementMinimum(len(m.rawRequirements))
	}

	return min(req.MinimumTermMatches, len(m.rawRequirements))
}

func exactSurfaceFieldTermPositions(
	req SearchRequest,
	fields search.FieldTermLocationMap,
) map[string]map[string][]int {
	positions := make(map[string]map[string][]int, len(fields))
	for field, terms := range fields {
		positions[field] = boundedFieldTermPositions(req, terms)
	}

	return positions
}

func sameStoredLocation(left *search.Location, right *search.Location) bool {
	return left != nil && right != nil && left.Pos == right.Pos &&
		left.ArrayPositions.Equals(right.ArrayPositions)
}
