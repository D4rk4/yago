package searchindex

import (
	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
)

func minimumTermsQuery(
	req SearchRequest,
	analyzers []string,
	weights RankingWeights,
) blevequery.Query {
	terms := requirableTerms(queryTermWords(req), analyzers)
	if len(terms) == 0 {
		return crossFieldTermClause(req.Query, analyzers, weights, 1)
	}
	minimum := min(max(1, req.MinimumTermMatches), len(terms))
	main := bleve.NewBooleanQuery()
	for _, term := range terms {
		main.AddShould(crossFieldTermClause(term, analyzers, weights, 1))
	}
	main.SetMinShould(float64(minimum))
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
