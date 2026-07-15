package searchcore

import "strings"

func orderedPositionEvidence(
	fields map[string]map[string][]int,
	requirements []rerankQueryRequirement,
) (float64, float64) {
	if len(requirements) < 2 {
		return 0, 0
	}
	exact := make([]bool, len(requirements)-1)
	agreement := make([]float64, len(requirements)-1)
	for _, positions := range fields {
		for index := range agreement {
			fieldExact, fieldAgreement := positionGapEvidence(
				positions[requirements[index].term],
				positions[requirements[index+1].term],
				requirements[index+1].ordinal-requirements[index].ordinal,
			)
			exact[index] = exact[index] || fieldExact
			agreement[index] = max(agreement[index], fieldAgreement)
		}
	}
	exactTotal := 0
	agreementTotal := 0.0
	for index, pairAgreement := range agreement {
		if exact[index] {
			exactTotal++
		}
		agreementTotal += pairAgreement
	}

	pairs := float64(len(agreement))

	return float64(exactTotal) / pairs, agreementTotal / pairs
}

func positionGapEvidence(left []int, right []int, expected int) (bool, float64) {
	if expected <= 0 {
		return false, 0
	}
	leftIndex := 0
	rightIndex := 0
	bestDeviation := -1
	for leftIndex < len(left) && rightIndex < len(right) {
		difference := right[rightIndex] - left[leftIndex]
		if difference <= 0 {
			rightIndex++

			continue
		}
		deviation := difference - expected
		if deviation < 0 {
			deviation = -deviation
		}
		if deviation == 0 {
			return true, 1
		}
		if bestDeviation < 0 || deviation < bestDeviation {
			bestDeviation = deviation
		}
		if difference < expected {
			rightIndex++
		} else {
			leftIndex++
		}
	}
	if bestDeviation < 0 {
		return false, 0
	}

	return false, 1 / (1 + float64(bestDeviation))
}

func orderedTextEvidence(
	text string,
	requirements []rerankQueryRequirement,
) (float64, float64) {
	if len(requirements) < 2 {
		return 0, 0
	}
	requirementTerms := make(map[string]bool, len(requirements))
	for _, requirement := range requirements {
		requirementTerms[requirement.term] = true
	}
	positions := make(map[string][]int, len(requirements))
	for ordinal, token := range strings.Fields(strings.ToLower(text)) {
		if requirementTerms[token] {
			positions[token] = append(positions[token], ordinal)
		}
	}

	return orderedPositionEvidence(
		map[string]map[string][]int{"text": positions},
		requirements,
	)
}
