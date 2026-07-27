package rankfit

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

// TestAscendRefusesCandidatesOutsideTheWeightCatalog pairs the two directions of
// the Validate admission guard on one search: urlPrior already sits at its
// catalog maximum, so every multiplicative step above it must be turned away,
// while title has room and its step must be taken. Without the guard the search
// would report a fitted profile the ranking layer refuses to load.
func TestAscendRefusesCandidatesOutsideTheWeightCatalog(t *testing.T) {
	start := searchindex.DefaultRankingWeights()
	rewardURLPrior := func(weights searchindex.RankingWeights) (float64, error) {
		return weights.URLPrior, nil
	}

	best, score, _, err := Ascend(start, rewardURLPrior, DefaultOptions())
	if err != nil {
		t.Fatalf("Ascend urlPrior: %v", err)
	}
	if best.URLPrior != start.URLPrior || score != start.URLPrior {
		t.Errorf(
			"urlPrior = %v at score %v, want the catalog maximum %v held",
			best.URLPrior,
			score,
			start.URLPrior,
		)
	}
	if err := best.Validate(); err != nil {
		t.Errorf("fitted weights are unloadable: %v", err)
	}

	rewardTitle := func(weights searchindex.RankingWeights) (float64, error) {
		return weights.Title, nil
	}
	best, _, _, err = Ascend(start, rewardTitle, DefaultOptions())
	if err != nil {
		t.Fatalf("Ascend title: %v", err)
	}
	if best.Title <= start.Title {
		t.Errorf("title = %v, want an in-catalog step above %v taken", best.Title, start.Title)
	}
	if err := best.Validate(); err != nil {
		t.Errorf("fitted title weights are unloadable: %v", err)
	}
}

// TestAscendLeavesEquallyScoringWeightsUnchanged pins the strict-improvement
// margin: a plateau objective must return the starting profile untouched after a
// single sweep. Without the margin the search would wander between candidates
// that score the same, so two fits of one judgment set would disagree.
func TestAscendLeavesEquallyScoringWeightsUnchanged(t *testing.T) {
	start := searchindex.DefaultRankingWeights()
	plateau := func(searchindex.RankingWeights) (float64, error) {
		return 0.5, nil
	}

	best, score, rounds, err := Ascend(start, plateau, DefaultOptions())
	if err != nil {
		t.Fatalf("Ascend: %v", err)
	}
	if best != start {
		t.Errorf("plateau fit moved the weights: %#v", best)
	}
	if score != 0.5 || rounds != 1 {
		t.Errorf("plateau fit = score %v over %d rounds, want 0.5 over 1", score, rounds)
	}
}
