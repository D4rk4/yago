package searchindex

import "slices"

func StoredEvidenceRequirementOrdinals(
	analyzer string,
	requirements []string,
) ([]int, bool) {
	if !StoredEvidenceAnalyzerAvailable(analyzer) || len(requirements) == 0 {
		return nil, false
	}
	matcher := newStoredEvidenceMatcher(SearchRequest{Terms: requirements}, analyzer)
	if matcher.name != analyzer {
		return nil, false
	}

	return slices.Clone(matcher.rawRequirementOrdinals), true
}

func absentDocumentRequirementOrdinals(
	relevant []int,
	fields map[string]map[int][]int,
) []int {
	present := make(map[int]struct{}, len(relevant))
	for _, requirements := range fields {
		for ordinal, positions := range requirements {
			if len(positions) > 0 {
				present[ordinal] = struct{}{}
			}
		}
	}
	absent := make([]int, 0, len(relevant))
	for _, ordinal := range relevant {
		if _, found := present[ordinal]; !found {
			absent = append(absent, ordinal)
		}
	}

	return absent
}
