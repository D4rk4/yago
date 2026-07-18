package yagonode

import (
	"context"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func (e *searchExplainEndpoint) Explain(
	ctx context.Context,
	query string,
	global bool,
) (adminui.SearchExplanation, error) {
	scope := searchcore.SourceLocal
	if global {
		scope = searchcore.SourceGlobal
	}
	response, _, err := e.explanation(ctx, searchExplainRequest{Query: query, Scope: scope})
	if err != nil {
		return adminui.SearchExplanation{}, err
	}
	results := make([]adminui.SearchExplanationResult, 0, len(response.Results))
	for index, result := range response.Results {
		results = append(results, adminSearchExplanationResult(index+1, result))
	}

	return adminui.SearchExplanation{
		Query:           response.Query,
		Global:          response.Scope == searchcore.SourceGlobal,
		ModelRevision:   response.ModelRevision,
		ModelKind:       string(response.ModelKind),
		PartialFailures: adminSearchPartialFailures(response.PartialFailures),
		Results:         results,
	}, nil
}

func adminSearchExplanationResult(
	finalRank int,
	result searchExplainResult,
) adminui.SearchExplanationResult {
	return adminui.SearchExplanationResult{
		FinalRank:           finalRank,
		URL:                 result.URL,
		Source:              result.Source,
		Score:               result.Score,
		RetrievalScore:      result.RetrievalScore,
		Quality:             result.Quality,
		QualityKnown:        result.QualityKnown,
		SpamRisk:            result.SpamRisk,
		SpamRiskKnown:       result.SpamRiskKnown,
		FunctionWordShare:   result.FunctionWordFraction,
		FunctionWordKnown:   result.FunctionWordKnown,
		SymbolShare:         result.SymbolFraction,
		SymbolKnown:         result.SymbolKnown,
		AlphabeticShare:     result.AlphabeticFraction,
		AlphabeticKnown:     result.AlphabeticKnown,
		UniqueTokenShare:    result.UniqueTokenFraction,
		UniqueTokenKnown:    result.UniqueTokenKnown,
		Proximity:           result.Proximity,
		ProximityKnown:      result.ProximityKnown,
		FieldContributions:  adminSearchFieldContributions(result.FieldScores),
		Evidence:            adminSearchRankingSignals(result.Evidence),
		Fusion:              adminSearchFusionContributions(result.Fusion),
		RetrievalDiagnostic: result.Explanation,
		Learned:             adminSearchLearnedExplanation(result.Learned),
	}
}

func adminSearchFusionContributions(
	contributions []searchExplainFusion,
) []adminui.SearchFusionContribution {
	result := make([]adminui.SearchFusionContribution, 0, len(contributions))
	for _, contribution := range contributions {
		result = append(result, adminui.SearchFusionContribution{
			Branch: contribution.Branch, Rank: contribution.Rank,
			Contribution: contribution.Contribution,
		})
	}

	return result
}

func adminSearchPartialFailures(failures []searchcore.PartialFailure) []string {
	result := make([]string, 0, len(failures))
	for _, failure := range failures {
		result = append(result, failure.SourceLabel()+": "+failure.Reason)
	}

	return result
}

func adminSearchRankingSignals(signals []searchExplainSignal) []adminui.SearchRankingSignal {
	result := make([]adminui.SearchRankingSignal, 0, len(signals))
	for _, signal := range signals {
		result = append(result, adminui.SearchRankingSignal{
			Name: signal.Name, Value: signal.Value,
		})
	}

	return result
}

func adminSearchFieldContributions(scores map[string]float64) []adminui.SearchFieldContribution {
	names := make([]string, 0, len(scores))
	for name := range scores {
		names = append(names, name)
	}
	sort.Strings(names)
	contributions := make([]adminui.SearchFieldContribution, 0, len(names))
	for _, name := range names {
		contributions = append(contributions, adminui.SearchFieldContribution{
			Name: name, Score: scores[name],
		})
	}

	return contributions
}

func adminSearchLearnedExplanation(
	explanation *learnedrank.ResultExplanation,
) *adminui.SearchLearnedExplanation {
	if explanation == nil {
		return nil
	}
	signals := make([]adminui.SearchLearnedSignal, 0, len(explanation.Signals))
	for _, signal := range explanation.Signals {
		signals = append(signals, adminui.SearchLearnedSignal{
			Name:            signal.Name,
			Known:           signal.Known,
			Value:           signal.Value,
			Used:            signal.Used,
			NormalizedValue: signal.NormalizedValue,
			Weight:          signal.Weight,
			Contribution:    signal.Contribution,
		})
	}
	trees := make([]adminui.SearchLearnedTree, 0, len(explanation.Trees))
	for _, tree := range explanation.Trees {
		trees = append(trees, adminSearchLearnedTree(tree))
	}

	return &adminui.SearchLearnedExplanation{
		OriginalRank:  explanation.OriginalRank,
		ModelRank:     explanation.ModelRank,
		FinalRank:     explanation.FinalRank,
		OriginalScore: explanation.OriginalScore,
		Score:         explanation.Score,
		Signals:       signals,
		Trees:         trees,
	}
}

func adminSearchLearnedTree(tree learnedrank.TreeExplanation) adminui.SearchLearnedTree {
	decisions := make([]adminui.SearchLearnedTreeDecision, 0, len(tree.Decisions))
	for _, decision := range tree.Decisions {
		decisions = append(decisions, adminui.SearchLearnedTreeDecision{
			Name:              decision.Name,
			Known:             decision.Known,
			TerminatedMissing: decision.TerminatedMissing,
			NormalizedValue:   decision.NormalizedValue,
			Threshold:         decision.Threshold,
			WentLeft:          decision.WentLeft,
		})
	}

	return adminui.SearchLearnedTree{
		Index:            tree.TreeIndex,
		InteractionGroup: tree.InteractionGroup,
		Contribution:     tree.Contribution,
		Decisions:        decisions,
	}
}
