package tavilyapi

func applyCanonicalRankScores(results []SearchResult) {
	if len(results) == 0 {
		return
	}
	denominator := float64(len(results))
	for index := range results {
		results[index].Score = float64(len(results)-index) / denominator
	}
}
