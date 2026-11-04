package searchindex

import (
	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"

	"github.com/D4rk4/yago/yagonode/internal/queryidentifier"
)

func queryWithMinimumTerms(
	terms []string,
	minimum int,
	clause func(string) blevequery.Query,
) blevequery.Query {
	query := bleve.NewBooleanQuery()
	optional := make([]blevequery.Query, 0, len(terms))
	for _, term := range terms {
		termClause := clause(term)
		if queryidentifier.MixedAlphanumeric(term) {
			query.AddMust(termClause)
		} else {
			optional = append(optional, termClause)
		}
	}
	identifiers := len(terms) - len(optional)
	query.Should = minimumMatchQuery(optional, max(0, minimum-identifiers))

	return query
}
