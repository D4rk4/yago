package searchindex

import "github.com/blevesearch/bleve/v2/search"

type storedCJKProximityEvidence struct {
	values         []string
	exact          search.TermLocationMap
	wordForms      search.TermLocationMap
	requirements   []string
	ordinals       []int
	allowWordForms bool
}

func storedCJKRequirementProximity(evidence storedCJKProximityEvidence) (float64, float64) {
	if len(evidence.requirements) < 2 {
		return 0, 0
	}
	available := evidence.exact
	if evidence.allowWordForms {
		available = evidence.wordForms
	}
	if storedRequirementsWithLocations(available, evidence.requirements) < 2 {
		return 0, 0
	}
	unordered := 0.0
	ordered := 0.0
	for index := 0; index+1 < len(evidence.requirements); index++ {
		left := evidence.requirements[index]
		right := evidence.requirements[index+1]
		expectedGap := storedRequirementGap(evidence.ordinals, index)
		exactUnordered := storedCJKLocationsWithinWindow(
			evidence.values,
			evidence.exact[left],
			evidence.exact[right],
			sdmUnorderedWindow,
		)
		exactOrdered := storedCJKLocationsAtGap(
			evidence.values,
			evidence.exact[left],
			evidence.exact[right],
			expectedGap,
		)
		wordFormUnordered := false
		wordFormOrdered := false
		if evidence.allowWordForms && (!exactUnordered || !exactOrdered) {
			if !exactUnordered {
				wordFormUnordered = storedCJKLocationsWithinWindow(
					evidence.values,
					evidence.wordForms[left],
					evidence.wordForms[right],
					sdmUnorderedWindow,
				)
			}
			if !exactOrdered {
				wordFormOrdered = storedCJKLocationsAtGap(
					evidence.values,
					evidence.wordForms[left],
					evidence.wordForms[right],
					expectedGap,
				)
			}
		}
		unordered += storedPairConfidence(exactUnordered, wordFormUnordered)
		ordered += storedPairConfidence(exactOrdered, wordFormOrdered)
	}
	pairs := float64(len(evidence.requirements) - 1)

	return unordered / pairs, ordered / pairs
}

func storedCJKLocationsWithinWindow(
	values []string,
	left search.Locations,
	right search.Locations,
	window int,
) bool {
	for _, leftLocation := range left {
		for _, rightLocation := range right {
			gap, found := storedCJKLocationGap(values, leftLocation, rightLocation)
			if found && gap <= storedLocationCoordinate(window) {
				return true
			}
		}
	}

	return false
}

func storedCJKLocationsAtGap(
	values []string,
	left search.Locations,
	right search.Locations,
	expected uint64,
) bool {
	for _, leftLocation := range left {
		for _, rightLocation := range right {
			gap, found := storedCJKOrderedLocationGap(
				values,
				leftLocation,
				rightLocation,
			)
			if found && gap == expected {
				return true
			}
		}
	}

	return false
}

func storedCJKLocationGap(
	values []string,
	left *search.Location,
	right *search.Location,
) (uint64, bool) {
	if gap, found := storedCJKOrderedLocationGap(values, left, right); found {
		return gap, true
	}

	return storedCJKOrderedLocationGap(values, right, left)
}

func storedCJKOrderedLocationGap(
	values []string,
	left *search.Location,
	right *search.Location,
) (uint64, bool) {
	if left == nil || right == nil ||
		!left.ArrayPositions.Equals(right.ArrayPositions) ||
		right.Start < left.End {
		return 0, false
	}
	value, found := storedLocationValue(values, left)
	if !found || right.Start > uint64(len(value)) {
		return 0, false
	}
	separator := value[left.End:right.Start]

	return storedLocationCoordinate(storedCJKSeparatorUnits(separator) + 1), true
}

func storedCJKSeparatorUnits(text string) int {
	units := 0
	for start, end := range rangeStoredTokens(text) {
		token := text[start:end]
		cjk := 0
		for _, character := range token {
			if storedCJKCharacter(character) {
				cjk++
			}
		}
		if cjk > 0 {
			units += cjk
		} else {
			units++
		}
	}

	return units
}
