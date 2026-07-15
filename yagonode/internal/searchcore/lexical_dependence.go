package searchcore

func lexicalDependenceComponents(
	result Result,
	terms []string,
	requirements []rerankQueryRequirement,
) (float64, float64, float64, float64) {
	coverage, proximity, positioned := lexicalComponentsFromPositions(
		result.FieldTermPositions,
		terms,
	)
	if positioned {
		ordered, gapAgreement := orderedPositionEvidence(
			result.FieldTermPositions,
			requirements,
		)

		return coverage, proximity, ordered, gapAgreement
	}
	text := result.Title + " " + result.Snippet
	coverage, proximity = lexicalTextComponents(text, terms)
	ordered, gapAgreement := orderedTextEvidence(text, requirements)

	return coverage, proximity, ordered, gapAgreement
}

func orderedPositionFraction(
	fields map[string]map[string][]int,
	requirements []rerankQueryRequirement,
) float64 {
	exact, _ := orderedPositionEvidence(fields, requirements)

	return exact
}

func positionsAtQueryDistance(left []int, right []int, distance int) bool {
	exact, _ := positionGapEvidence(left, right, distance)

	return exact
}

func orderedTextFraction(text string, requirements []rerankQueryRequirement) float64 {
	exact, _ := orderedTextEvidence(text, requirements)

	return exact
}
