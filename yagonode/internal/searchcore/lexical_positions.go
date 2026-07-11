package searchcore

import "sort"

func LexicalPositions(results []Result, offset int) []int {
	positions := make([]int, len(results))
	localSlots := make([]int, 0, len(results))
	localCandidates := make([]int, 0, len(results))
	for index, result := range results {
		positions[index] = offset + index + 1
		if result.StoredLocally() {
			localSlots = append(localSlots, index)
			localCandidates = append(localCandidates, index)
		}
	}
	sort.SliceStable(localCandidates, func(left, right int) bool {
		leftRank, leftKnown := results[localCandidates[left]].Evidence.Value(SignalLocalRank)
		rightRank, rightKnown := results[localCandidates[right]].Evidence.Value(SignalLocalRank)
		if leftKnown != rightKnown {
			return leftKnown
		}
		if leftKnown && leftRank != rightRank {
			return leftRank < rightRank
		}

		return localCandidates[left] < localCandidates[right]
	})
	for order, candidate := range localCandidates {
		positions[candidate] = offset + localSlots[order] + 1
	}

	return positions
}
