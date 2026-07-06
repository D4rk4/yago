package searchcore

import "sort"

// rrfK is the standard Reciprocal Rank Fusion constant (Cormack et al., SIGIR
// 2009): large enough that deep ranks still contribute, small enough that top
// ranks dominate.
const rrfK = 60

// FuseByReciprocalRank merges independently-ranked result lists with
// Reciprocal Rank Fusion: each list contributes 1/(k+rank) per result, scores
// from different sources are never compared directly, and a result found by
// several sources rises. Rank-based fusion sidesteps the incomparable-score
// problem of heterogeneous peers entirely (no calibration needed) and is the
// merge primitive any future dense retrieval layer plugs into. Result
// identity follows the wire identity (URL hash, then URL); the first list
// carrying a result provides its display fields, later lists only add fused
// weight. The fused weight replaces Score so downstream ordering, paging, and
// diversification keep working unchanged.
func FuseByReciprocalRank(lists ...[]Result) []Result {
	fused := make([]Result, 0, totalResults(lists))
	weights := map[string]float64{}
	position := map[string]int{}
	for _, list := range lists {
		for rank, result := range list {
			key := resultIdentity(result)
			if _, exists := weights[key]; !exists {
				position[key] = len(fused)
				fused = append(fused, result)
			}
			weights[key] += 1.0 / float64(rrfK+rank+1)
		}
	}
	for key, index := range position {
		fused[index].Score = weights[key]
	}
	sort.SliceStable(fused, func(i, j int) bool {
		return fused[i].Score > fused[j].Score
	})

	return fused
}

func totalResults(lists [][]Result) int {
	total := 0
	for _, list := range lists {
		total += len(list)
	}

	return total
}
