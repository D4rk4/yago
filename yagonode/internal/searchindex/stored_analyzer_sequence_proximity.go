package searchindex

import "github.com/blevesearch/bleve/v2/search"

func storedSingleRequirementAnalyzerProximity(
	matcher *storedEvidenceMatcher,
	targets map[int]search.Locations,
	allowAnalyzerSequence bool,
) (float64, float64) {
	if !allowAnalyzerSequence || len(matcher.rawRequirements) != 1 ||
		len(matcher.targets) < 2 {
		return 0, 0
	}
	lastTarget := min(len(matcher.targets), maximumTermPositionsPerField)
	unordered := 0
	ordered := 0
	for index := 0; index+1 < lastTarget; index++ {
		if storedLocationsWithinWindow(
			targets[index],
			targets[index+1],
			sdmUnorderedWindow,
		) {
			unordered++
		}
		if storedLocationsAtGap(targets[index], targets[index+1], 1) {
			ordered++
		}
	}
	pairs := lastTarget - 1
	confidence := analyzerVariantPairConfidence / float64(pairs)

	return float64(unordered) * confidence, float64(ordered) * confidence
}
