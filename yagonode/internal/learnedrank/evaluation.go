package learnedrank

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/rankfit"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type rankingCandidate struct {
	originalIndex      int
	documentIdentifier string
	identity           string
	result             searchcore.Result
	features           rankfit.FeatureVector
}

type candidateEvaluation struct {
	documentIdentifier string
	score              float64
	rank               int
	signals            []SignalExplanation
	trees              []TreeExplanation
}

func (r *Ranker) Rerank(
	request searchcore.Request,
	results []searchcore.Result,
) (Outcome, error) {
	outcome := Outcome{Results: cloneResults(results)}
	snapshot := r.active.Load()
	if snapshot == nil || len(results) == 0 {
		return outcome, nil
	}
	outcome.SnapshotRevision = snapshot.revision
	outcome.ModelKind = snapshot.kind
	candidates, err := rankingCandidates(results, r.candidateWindow)
	if err != nil {
		return outcome, err
	}
	if len(candidates) < 2 {
		return outcome, nil
	}
	group, err := rankingQueryGroup(request, candidates)
	if err != nil {
		return outcome, err
	}
	evaluations, err := snapshot.evaluate(group, candidates, request.Explain)
	if err != nil {
		return outcome, err
	}
	identifiers, explanations := applyCandidateEvaluations(
		outcome.Results,
		candidates,
		evaluations,
		request.Explain,
	)
	outcome.Explanations = explanations
	if request.Explain {
		finalRanks := make(map[string]int, len(candidates))
		for index, identifier := range identifiers {
			if identifier != "" {
				finalRanks[identifier] = index + 1
			}
		}
		for index := range outcome.Explanations {
			identifier := outcome.Explanations[index].documentIdentifier
			outcome.Explanations[index].FinalRank = finalRanks[identifier]
		}
		slices.SortFunc(outcome.Explanations, func(left, right ResultExplanation) int {
			return left.FinalRank - right.FinalRank
		})
	}
	outcome.Applied = true

	return outcome, nil
}

func cloneResults(results []searchcore.Result) []searchcore.Result {
	if results == nil {
		return nil
	}

	return append(make([]searchcore.Result, 0, len(results)), results...)
}

func applyCandidateEvaluations(
	results []searchcore.Result,
	candidates []rankingCandidate,
	evaluations []candidateEvaluation,
	explain bool,
) ([]string, []ResultExplanation) {
	identifiers := make([]string, len(results))
	slots := make([]int, len(candidates))
	byIdentifier := make(map[string]rankingCandidate, len(candidates))
	for index, candidate := range candidates {
		slots[index] = candidate.originalIndex
		byIdentifier[candidate.documentIdentifier] = candidate
	}
	var explanations []ResultExplanation
	if explain {
		explanations = make([]ResultExplanation, 0, len(evaluations))
	}
	for index, evaluation := range evaluations {
		candidate := byIdentifier[evaluation.documentIdentifier]
		result := candidate.result
		result.Score = evaluation.score
		result = searchcore.WithDiversityRelevance(result, evaluation.score)
		position := slots[index]
		results[position] = result
		identifiers[position] = evaluation.documentIdentifier
		if explain {
			explanations = append(explanations, ResultExplanation{
				Identity:           candidate.identity,
				OriginalRank:       candidate.originalIndex + 1,
				ModelRank:          evaluation.rank,
				OriginalScore:      candidate.result.Score,
				Score:              evaluation.score,
				Signals:            evaluation.signals,
				Trees:              evaluation.trees,
				documentIdentifier: evaluation.documentIdentifier,
			})
		}
	}

	return identifiers, explanations
}

func rankingCandidates(
	results []searchcore.Result,
	candidateWindow int,
) ([]rankingCandidate, error) {
	candidates := make([]rankingCandidate, 0, min(len(results), candidateWindow))
	localSlots := 0
	for index, result := range results {
		if !result.StoredLocally() {
			continue
		}
		localSlots++
		if localSlots > candidateWindow {
			break
		}
		features, known, err := MapRankingEvidence(result.Evidence)
		if err != nil {
			return nil, fmt.Errorf("candidate %d ranking evidence: %w", index+1, err)
		}
		if !known {
			continue
		}
		identity := rankingIdentity(result, index)
		candidates = append(candidates, rankingCandidate{
			originalIndex:      index,
			documentIdentifier: identity + "\x00" + fmt.Sprintf("%03d", index),
			identity:           identity,
			result:             result,
			features:           features,
		})
	}

	return candidates, nil
}

func rankingIdentity(result searchcore.Result, index int) string {
	if result.URLHash != "" {
		return "hash:" + result.URLHash
	}
	if result.URL != "" {
		return "url:" + result.URL
	}
	if result.DisplayURL != "" {
		return "display_url:" + result.DisplayURL
	}
	if result.Title != "" {
		return "title:" + result.Title
	}

	return "position:" + strconv.Itoa(index+1)
}

func rankingQueryGroup(
	request searchcore.Request,
	candidates []rankingCandidate,
) (rankfit.QueryGroup, error) {
	examples := make([]rankfit.RankingExample, len(candidates))
	for index, candidate := range candidates {
		example, err := rankfit.NewRankingExample(
			candidate.documentIdentifier,
			0,
			candidate.features,
		)
		if err != nil {
			return rankfit.QueryGroup{}, fmt.Errorf("build ranking candidate: %w", err)
		}
		examples[index] = example
	}
	identifier := request.Query
	if identifier == "" {
		identifier = strings.Join(request.Terms, "\x00")
	}
	if identifier == "" {
		identifier = "learned-rank-query"
	}
	group, err := rankfit.NewQueryGroup(identifier, examples)
	if err != nil {
		return rankfit.QueryGroup{}, fmt.Errorf("build learned ranking query: %w", err)
	}

	return group, nil
}
