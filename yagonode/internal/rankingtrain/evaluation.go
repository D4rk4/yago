package rankingtrain

import (
	"context"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searcheval"
)

func evaluateRankingModel(
	ctx context.Context,
	model trainedRankingModel,
	incumbent *trainedRankingModel,
	datasets []queryDataset,
	clocks ...func() time.Time,
) (EvaluationComparison, error) {
	clock := evaluationClock(clocks)
	baseline := make([]searcheval.QueryObservation, len(datasets))
	candidate := make([]searcheval.QueryObservation, len(datasets))
	var active []searcheval.QueryObservation
	if incumbent != nil {
		active = make([]searcheval.QueryObservation, len(datasets))
	}
	for index, dataset := range datasets {
		if err := ctx.Err(); err != nil {
			return EvaluationComparison{}, fmt.Errorf("evaluate ranking model: %w", err)
		}
		baseline[index] = baselineObservation(dataset, clock)
		if incumbent != nil {
			observation, incumbentErr := learnedObservation(*incumbent, dataset, clock)
			if incumbentErr != nil {
				return EvaluationComparison{}, fmt.Errorf(
					"rank query %q with active incumbent: %w",
					dataset.judgment.Query,
					incumbentErr,
				)
			}
			active[index] = observation
		}
		observation, err := learnedObservation(model, dataset, clock)
		if err != nil {
			return EvaluationComparison{}, fmt.Errorf(
				"rank query %q: %w",
				dataset.judgment.Query,
				err,
			)
		}
		candidate[index] = observation
	}

	return evaluationComparison(baseline, active, candidate, incumbent != nil)
}

func evaluationClock(clocks []func() time.Time) func() time.Time {
	if len(clocks) != 0 && clocks[0] != nil {
		return clocks[0]
	}

	return time.Now
}

func baselineObservation(
	dataset queryDataset,
	clock func() time.Time,
) searcheval.QueryObservation {
	started := clock()
	results := searchcore.DiversifyResults(
		cloneSearchResults(dataset.results),
		dataset.request,
	)

	return rankedObservation(dataset, results, max(clock().Sub(started), 0))
}

func learnedObservation(
	model trainedRankingModel,
	dataset queryDataset,
	clock func() time.Time,
) (searcheval.QueryObservation, error) {
	started := clock()
	results, err := modelRankedResults(model, dataset)
	if err != nil {
		return searcheval.QueryObservation{}, err
	}
	results = searchcore.DiversifyResults(results, dataset.request)

	return rankedObservation(dataset, results, max(clock().Sub(started), 0)), nil
}

func rankedObservation(
	dataset queryDataset,
	results []searchcore.Result,
	latency time.Duration,
) searcheval.QueryObservation {
	return searcheval.QueryObservation{
		ID:            dataset.judgment.Query,
		Judgment:      dataset.judgment,
		Candidates:    canonicalRankedCandidates(results),
		RerankLatency: latency,
	}
}

func evaluationComparison(
	baseline []searcheval.QueryObservation,
	incumbent []searcheval.QueryObservation,
	candidate []searcheval.QueryObservation,
	hasIncumbent bool,
) (EvaluationComparison, error) {
	comparison := EvaluationComparison{}
	if hasIncumbent {
		incumbentReport, incumbentErr := searcheval.EvaluateHeldout(incumbent)
		if incumbentErr != nil {
			return EvaluationComparison{}, fmt.Errorf(
				"evaluate active incumbent ranking: %w",
				incumbentErr,
			)
		}
		comparison.Incumbent = &incumbentReport
	}
	baselineReport, err := searcheval.EvaluateHeldout(baseline)
	if err != nil {
		return EvaluationComparison{}, fmt.Errorf("evaluate baseline ranking: %w", err)
	}
	candidateReport, err := searcheval.EvaluateHeldout(candidate)
	if err != nil {
		return EvaluationComparison{}, fmt.Errorf("evaluate candidate ranking: %w", err)
	}

	comparison.Baseline = baselineReport
	comparison.Candidate = candidateReport

	return comparison, nil
}

func modelRankedResults(
	model trainedRankingModel,
	dataset queryDataset,
) ([]searchcore.Result, error) {
	ranked := cloneSearchResults(dataset.results)
	if !dataset.hasGroup || len(dataset.modelCandidates) < 2 {
		return ranked, nil
	}
	predictions, err := model.predict(dataset.group)
	if err != nil {
		return nil, err
	}
	byIdentity := make(map[string]searchcore.Result, len(dataset.modelCandidates))
	slots := make([]int, len(dataset.modelCandidates))
	for index, candidate := range dataset.modelCandidates {
		byIdentity[candidate.identity] = ranked[candidate.slot]
		slots[index] = candidate.slot
	}
	for index, prediction := range predictions {
		candidate := byIdentity[prediction.DocumentIdentifier]
		candidate.Score = prediction.Score
		candidate = searchcore.WithDiversityRelevance(candidate, prediction.Score)
		ranked[slots[index]] = candidate
	}

	return ranked, nil
}

func canonicalRankedCandidates(results []searchcore.Result) []searcheval.RankedCandidate {
	candidates := make([]searcheval.RankedCandidate, len(results))
	for index, result := range results {
		candidates[index] = canonicalRankedCandidate(result, rankingCandidateIdentity(result))
	}

	return candidates
}

func cloneSearchResults(results []searchcore.Result) []searchcore.Result {
	return append([]searchcore.Result(nil), results...)
}
