package searchindex

import (
	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

func minimumTermsQuery(
	req SearchRequest,
	analyzers []string,
	weights RankingWeights,
	analyzerScope bool,
) blevequery.Query {
	if !analyzerScope {
		return strictMinimumTermsQuery(req, analyzers, weights)
	}
	branchRequest := req
	branchRequest.ExpansionTerms = nil
	branches := []blevequery.Query{strictMinimumTermsQuery(
		branchRequest,
		[]string{standardTextAnalyzer},
		weights,
	)}
	for _, analyzer := range analyzers {
		terms := requirableTermsForAnalyzer(queryTermWords(req), analyzer)
		if len(terms) == 0 {
			continue
		}
		minimum := minimumTermRequirement(req, len(terms))
		matches := queryWithMinimumTerms(terms, minimum, func(term string) blevequery.Query {
			return crossFieldTermClauseForAnalyzer(term, analyzer, weights, 1)
		})
		branches = append(branches, bleve.NewConjunctionQuery(
			analyzerScopeClause(analyzer),
			matches,
		))
	}
	main := branches[0]
	if len(branches) > 1 {
		main = bleve.NewDisjunctionQuery(branches...)
	}
	if len(req.ExpansionTerms) == 0 {
		return main
	}
	query := bleve.NewBooleanQuery()
	query.AddMust(main)
	for _, term := range req.ExpansionTerms {
		query.AddShould(crossFieldTermClause(term, analyzers, weights, expansionBoostFactor))
	}

	return query
}

func strictMinimumTermsQuery(
	req SearchRequest,
	analyzers []string,
	weights RankingWeights,
) blevequery.Query {
	terms := requirableTerms(queryTermWords(req), analyzers)
	if len(terms) == 0 {
		return crossFieldTermClause(req.Query, analyzers, weights, 1)
	}
	minimum := minimumTermRequirement(req, len(terms))
	main := queryWithMinimumTerms(terms, minimum, func(term string) blevequery.Query {
		return crossFieldTermClause(term, analyzers, weights, 1)
	})
	if len(req.ExpansionTerms) == 0 {
		return main
	}

	query := bleve.NewBooleanQuery()
	query.AddMust(main)
	for _, term := range req.ExpansionTerms {
		query.AddShould(crossFieldTermClause(term, analyzers, weights, expansionBoostFactor))
	}

	return query
}

func minimumTermRequirement(req SearchRequest, requirements int) int {
	if req.Relaxed {
		return relaxedRequirementMinimum(requirements)
	}

	return min(max(1, req.MinimumTermMatches), requirements)
}
