package searchindex

import "github.com/blevesearch/bleve/v2/search"

const analyzerVariantPairConfidence = 0.5

type storedRawRequirement struct {
	term    string
	ordinal int
}

func storedWordFormProximity(
	exact search.TermLocationMap,
	wordForms search.TermLocationMap,
	requirements []string,
	ordinals []int,
	allowWordForms bool,
) (float64, float64) {
	if len(requirements) < 2 {
		return 0, 0
	}
	available := exact
	if allowWordForms {
		available = wordForms
	}
	if storedRequirementsWithLocations(available, requirements) < 2 {
		return 0, 0
	}
	unordered := 0.0
	ordered := 0.0
	for index := 0; index+1 < len(requirements); index++ {
		left := requirements[index]
		right := requirements[index+1]
		expectedGap := storedRequirementGap(ordinals, index)
		exactLeft := exact[left]
		exactRight := exact[right]
		exactUnordered := storedLocationsWithinWindow(
			exactLeft,
			exactRight,
			sdmUnorderedWindow,
		)
		exactOrdered := storedLocationsAtGap(exactLeft, exactRight, expectedGap)
		wordFormUnordered := false
		wordFormOrdered := false
		if allowWordForms && (!exactUnordered || !exactOrdered) {
			wordFormLeft := wordForms[left]
			wordFormRight := wordForms[right]
			if !exactUnordered {
				wordFormUnordered = storedLocationsWithinWindow(
					wordFormLeft,
					wordFormRight,
					sdmUnorderedWindow,
				)
			}
			if !exactOrdered {
				wordFormOrdered = storedLocationsAtGap(
					wordFormLeft,
					wordFormRight,
					expectedGap,
				)
			}
		}
		unordered += storedPairConfidence(exactUnordered, wordFormUnordered)
		ordered += storedPairConfidence(exactOrdered, wordFormOrdered)
	}
	pairs := float64(len(requirements) - 1)

	return unordered / pairs, ordered / pairs
}

func storedRequirementsWithLocations(
	locations search.TermLocationMap,
	requirements []string,
) int {
	present := 0
	for _, requirement := range requirements {
		if len(locations[requirement]) == 0 {
			continue
		}
		present++
		if present == 2 {
			return present
		}
	}

	return present
}

func storedRequirementGap(ordinals []int, index int) uint64 {
	return storedLocationCoordinate(ordinals[index+1] - ordinals[index])
}

func storedPairConfidence(exact bool, wordForm bool) float64 {
	if exact {
		return 1
	}
	if wordForm {
		return analyzerVariantPairConfidence
	}

	return 0
}

func storedLocationsWithinWindow(
	left search.Locations,
	right search.Locations,
	window int,
) bool {
	leftIndex := 0
	rightIndex := 0
	maximumGap := storedLocationCoordinate(window)
	for leftIndex < len(left) && rightIndex < len(right) {
		leftPosition := left[leftIndex].Pos
		rightPosition := right[rightIndex].Pos
		switch {
		case leftPosition > rightPosition:
			if leftPosition-rightPosition <= maximumGap &&
				left[leftIndex].ArrayPositions.Equals(right[rightIndex].ArrayPositions) {
				return true
			}
			rightIndex++
		default:
			if rightPosition-leftPosition <= maximumGap &&
				left[leftIndex].ArrayPositions.Equals(right[rightIndex].ArrayPositions) {
				return true
			}
			leftIndex++
		}
	}

	return false
}

func storedLocationsAtGap(
	left search.Locations,
	right search.Locations,
	expectedGap uint64,
) bool {
	leftIndex := 0
	rightIndex := 0
	for leftIndex < len(left) && rightIndex < len(right) {
		leftPosition := left[leftIndex].Pos
		rightPosition := right[rightIndex].Pos
		if rightPosition >= leftPosition && rightPosition-leftPosition == expectedGap &&
			left[leftIndex].ArrayPositions.Equals(right[rightIndex].ArrayPositions) {
			return true
		}
		if rightPosition <= leftPosition || rightPosition-leftPosition < expectedGap {
			rightIndex++
		} else {
			leftIndex++
		}
	}

	return false
}
