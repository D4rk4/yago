package searchindex

import "github.com/blevesearch/bleve/v2/search"

func (f *storedFieldEvidence) exactRequirementLocation(
	matcher *storedEvidenceMatcher,
	target storedEvidenceTarget,
	location *search.Location,
) *search.Location {
	if target.rawRequirement == 0 ||
		target.rawRequirement >= len(matcher.rawRequirementOrdinals) {
		return location
	}
	previousRequirement := target.rawRequirement - 1
	if matcher.rawRequirementOrdinals[target.rawRequirement]-
		matcher.rawRequirementOrdinals[previousRequirement] != 1 {
		return location
	}
	previous := f.exactLatest[previousRequirement]
	if previous == nil || previous.End != location.Start ||
		!previous.ArrayPositions.Equals(location.ArrayPositions) {
		return location
	}
	projected := *location
	projected.Pos = previous.Pos + 1
	projected.ArrayPositions = append(
		search.ArrayPositions(nil),
		location.ArrayPositions...,
	)

	return &projected
}
