package searchcore

func finalizeRankingPayload(results []Result, explain bool) {
	for index := range results {
		results[index].FieldTermPositions = nil
		results[index].EvidenceRequirementOrdinals = nil
		if !explain {
			results[index].FieldScores = nil
			results[index].Explanation = ""
		}
	}
}
