// Package rankfit fits the ranking weight profile to measured relevance
// (YagoRank, ADR-0035). Coordinate ascent (Metzler & Croft, SIGIR 2007) does a
// coordinate-wise line search over the RankingWeights that today are hand-set,
// maximizing an objective — mean NDCG@k over a judgment set — so the weights are
// chosen by evidence rather than guessed. It is offline and pure: no query-time
// cost, no dependency, no wire change.
package rankfit

import "github.com/D4rk4/yago/yagonode/internal/searchindex"

// scoreEpsilon requires a strict improvement before moving, so the search cannot
// cycle between equally-scoring candidates.
const scoreEpsilon = 1e-9

// Objective scores a candidate weight vector; the learner maximizes it. It may
// fail (e.g. the underlying searcher errors), which aborts the search.
type Objective func(searchindex.RankingWeights) (float64, error)

// Options tune the coordinate-ascent search.
type Options struct {
	// Steps are the multiplicative moves tried on a positive weight.
	Steps []float64
	// SeedValues are the values tried on a weight currently at zero, so a
	// disabled prior can be switched back on.
	SeedValues []float64
	// MaxRounds bounds the sweeps over all dimensions.
	MaxRounds int
	// MinGain stops the search once a full sweep improves the objective by less
	// than this.
	MinGain float64
	// K is the rank cutoff the NDCG objective scores at.
	K int
}

// DefaultOptions is a conservative search: halve/double-ish steps, a few seeds
// for re-enabling a zeroed prior, and an early stop once gains flatten.
func DefaultOptions() Options {
	return Options{
		Steps:      []float64{0.5, 0.8, 1.25, 2.0},
		SeedValues: []float64{0.1, 0.25, 0.5},
		MaxRounds:  8,
		MinGain:    1e-4,
		K:          10,
	}
}

type dimension struct {
	key string
}

func (dimension dimension) get(weights searchindex.RankingWeights) float64 {
	value, _ := weights.Value(dimension.key)

	return value
}

func (dimension dimension) set(weights *searchindex.RankingWeights, value float64) {
	weights.Set(dimension.key, value)
}

// dimensions enumerates every tunable weight in a fixed order so the search is
// deterministic and reproducible.
var dimensions = rankingDimensions()

func rankingDimensions() []dimension {
	definitions := searchindex.RankingWeightDefinitions()
	dimensions := make([]dimension, 0, len(definitions))
	for _, definition := range definitions {
		dimensions = append(dimensions, dimension{key: definition.Key})
	}

	return dimensions
}

// Ascend runs coordinate ascent from start, returning the best weights found,
// their objective score, and the number of sweeps performed. A start whose
// objective cannot be scored, or any candidate that fails to score, returns the
// error.
func Ascend(
	start searchindex.RankingWeights,
	objective Objective,
	opts Options,
) (searchindex.RankingWeights, float64, int, error) {
	best := start
	bestScore, err := objective(best)
	if err != nil {
		return start, 0, 0, err
	}
	rounds := 0
	for round := 0; round < opts.MaxRounds; round++ {
		roundStart := bestScore
		for _, dim := range dimensions {
			best, bestScore, err = bestAlongDimension(best, bestScore, dim, objective, opts)
			if err != nil {
				return start, 0, 0, err
			}
		}
		rounds++
		if bestScore-roundStart < opts.MinGain {
			break
		}
	}

	return best, bestScore, rounds, nil
}

// bestAlongDimension moves one weight to the best-scoring valid candidate, the
// other weights held fixed.
func bestAlongDimension(
	best searchindex.RankingWeights,
	bestScore float64,
	dim dimension,
	objective Objective,
	opts Options,
) (searchindex.RankingWeights, float64, error) {
	for _, candidate := range candidates(dim.get(best), opts) {
		trial := best
		dim.set(&trial, candidate)
		if trial.Validate() != nil {
			continue
		}
		score, err := objective(trial)
		if err != nil {
			return best, bestScore, err
		}
		if score > bestScore+scoreEpsilon {
			best, bestScore = trial, score
		}
	}

	return best, bestScore, nil
}

// candidates are the values to try for one weight: always zero (to disable it),
// then multiplicative steps around a positive value or the seed values for a
// weight currently at zero.
func candidates(current float64, opts Options) []float64 {
	values := make([]float64, 0, len(opts.Steps)+len(opts.SeedValues)+1)
	values = append(values, 0)
	if current > 0 {
		for _, step := range opts.Steps {
			values = append(values, current*step)
		}

		return values
	}

	return append(values, opts.SeedValues...)
}
