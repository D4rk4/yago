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
	if result.EvidenceReady {
		return 0, 0, 0, 0
	}
	text := result.Title + " " + result.Snippet
	coverage, proximity = lexicalTextComponents(text, terms)
	ordered, gapAgreement := orderedTextEvidence(text, requirements)

	return coverage, proximity, ordered, gapAgreement
}
