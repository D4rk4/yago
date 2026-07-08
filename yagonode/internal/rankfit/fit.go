package rankfit

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

// SearcherFactory builds a searcher that ranks with the given weights, letting
// the learner score a candidate weight vector without disturbing the live
// ranking profile that serves real queries.
type SearcherFactory func(searchindex.RankingWeights) searchcore.Searcher

// Report is the outcome of a fit: the starting and fitted weights and their mean
// NDCG@k, plus how many sweeps ran.
type Report struct {
	Before     searchindex.RankingWeights
	After      searchindex.RankingWeights
	BeforeNDCG float64
	AfterNDCG  float64
	Rounds     int
}

// Improved reports whether the fitted weights out-scored the starting weights.
func (r Report) Improved() bool {
	return r.AfterNDCG > r.BeforeNDCG
}

// Fit fits the ranking weights to the judgments by coordinate ascent, scoring
// each candidate as the mean NDCG@k of a searcher the factory builds with those
// weights. It returns the before and after weights and scores. It requires at
// least one judgment, since with none the objective is undefined.
func Fit(
	ctx context.Context,
	start searchindex.RankingWeights,
	judgments []searcheval.Judgment,
	factory SearcherFactory,
	opts Options,
) (Report, error) {
	if len(judgments) == 0 {
		return Report{}, fmt.Errorf("fit needs at least one judgment")
	}
	objective := func(weights searchindex.RankingWeights) (float64, error) {
		report, err := searcheval.Evaluate(ctx, factory(weights), judgments, opts.K)
		if err != nil {
			return 0, fmt.Errorf("evaluate candidate weights: %w", err)
		}

		return report.Mean, nil
	}

	before, err := objective(start)
	if err != nil {
		return Report{}, err
	}
	best, bestScore, rounds, err := Ascend(start, objective, opts)
	if err != nil {
		return Report{}, err
	}

	return Report{
		Before:     start,
		After:      best,
		BeforeNDCG: before,
		AfterNDCG:  bestScore,
		Rounds:     rounds,
	}, nil
}
