package searchcore

func WithDiversityRelevance(result Result, relevance float64) Result {
	result.diversityRelevance = relevance
	result.diversityRelevanceSet = true

	return result
}

func DiversityRelevance(result Result) float64 {
	return resultDiversityRelevance(result)
}
