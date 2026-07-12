package searchcore

import (
	"cmp"
	"slices"
	"strings"
)

// rrfK is the standard Reciprocal Rank Fusion constant (Cormack et al., SIGIR
// 2009): large enough that deep ranks still contribute, small enough that top
// ranks dominate.
const rrfK = 60

type reciprocalRankFusion struct {
	results   []Result
	weights   map[string]float64
	positions map[string]int
}

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
	fusion := reciprocalRankFusion{
		results:   make([]Result, 0, totalResults(lists)),
		weights:   map[string]float64{},
		positions: map[string]int{},
	}
	for _, list := range lists {
		fusion.addRankedList(list)
	}
	for key, index := range fusion.positions {
		fusion.results[index].Score = fusion.weights[key]
	}
	slices.SortStableFunc(fusion.results, func(left, right Result) int {
		return cmp.Or(
			cmp.Compare(right.Score, left.Score),
			strings.Compare(resultIdentity(left), resultIdentity(right)),
		)
	})

	return fusion.results
}

func (f *reciprocalRankFusion) addRankedList(list []Result) {
	seen := make(map[string]struct{}, len(list))
	for rank, result := range list {
		key := resultIdentity(result)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = reciprocalRankResult(result, rank)
		f.mergeResult(key, result)
		f.weights[key] += 1.0 / float64(rrfK+rank+1)
	}
}

func (f *reciprocalRankFusion) mergeResult(key string, result Result) {
	if _, exists := f.weights[key]; !exists {
		f.positions[key] = len(f.results)
		f.results = append(f.results, result)

		return
	}
	index := f.positions[key]
	existingPeerSupport, hasExistingPeerSupport := f.results[index].Evidence.Value(
		SignalPeerSupport,
	)
	f.results[index].Evidence = f.results[index].Evidence.Overlay(result.Evidence)
	f.results[index].Evidence = f.results[index].Evidence.Add(SignalSourceCount, 1)
	if result.Source == SourceRemote && hasExistingPeerSupport {
		incomingPeerSupport, _ := result.Evidence.Value(SignalPeerSupport)
		f.results[index].Evidence = f.results[index].Evidence.With(
			SignalPeerSupport,
			existingPeerSupport+incomingPeerSupport,
		)
	}
}

func reciprocalRankResult(result Result, rank int) Result {
	result.Evidence = result.Evidence.With(SignalSourceCount, 1)
	if result.Source == SourceRemote {
		result.Evidence = result.Evidence.With(SignalRemoteRank, float64(rank+1))
		if _, known := result.Evidence.Value(SignalPeerSupport); !known {
			result.Evidence = result.Evidence.With(SignalPeerSupport, 1)
		}

		return result
	}
	if result.StoredLocally() {
		result.Evidence = result.Evidence.With(SignalLocalRank, float64(rank+1))
	}

	return result
}

func totalResults(lists [][]Result) int {
	total := 0
	for _, list := range lists {
		total += len(list)
	}

	return total
}
