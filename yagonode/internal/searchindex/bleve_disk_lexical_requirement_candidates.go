package searchindex

import (
	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

func bleveLexicalRequirementCandidateQuery(
	req SearchRequest,
	multilingual bool,
) blevequery.Query {
	req.Terms = distinctLexicalCandidateTerms(req)
	req.ExpansionTerms = nil
	weights := req.Weights.orDefault()
	analyzers := lexicalRequirementCandidateAnalyzers(req, multilingual)
	branches := make([]blevequery.Query, 0, len(analyzers))
	for _, analyzer := range analyzers {
		if analyzer != standardTextAnalyzer &&
			len(requirableTermsForAnalyzer(queryTermWords(req), analyzer)) == 0 {
			continue
		}
		branches = append(
			branches,
			bleveLexicalRequirementCandidateBranch(req, analyzer, weights),
		)
	}
	if len(branches) == 1 {
		return branches[0]
	}

	return bleve.NewDisjunctionQuery(branches...)
}

func lexicalRequirementCandidateAnalyzers(
	req SearchRequest,
	multilingual bool,
) []string {
	if !multilingual {
		return []string{""}
	}
	analyzers, _ := requirementAnalyzerBranches(
		req,
		queryAnalyzers(queryAnalyzerText(req)),
	)
	selected := make([]string, 0, len(analyzers)+1)
	seen := make(map[string]struct{}, len(analyzers)+1)
	for _, analyzer := range append([]string{standardTextAnalyzer}, analyzers...) {
		if _, found := seen[analyzer]; found {
			continue
		}
		seen[analyzer] = struct{}{}
		selected = append(selected, analyzer)
	}

	return selected
}

func bleveLexicalRequirementCandidateBranch(
	req SearchRequest,
	analyzer string,
	weights RankingWeights,
) blevequery.Query {
	analyzers := []string{analyzer}
	switch {
	case req.Fuzzy:
		return strictFuzzyRecoveryQuery(req, analyzers, weights)
	case req.Relaxed || req.MinimumTermMatches > 0:
		return strictMinimumTermsQuery(req, analyzers, weights)
	default:
		return strictRequiredTermsQuery(req, analyzers, weights)
	}
}
