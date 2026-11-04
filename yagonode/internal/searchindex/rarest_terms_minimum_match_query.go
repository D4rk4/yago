package searchindex

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type rarestTermsMinimumMatchQuery struct {
	clauses []blevequery.Query
	minimum int
}

func minimumMatchQuery(clauses []blevequery.Query, minimum int) blevequery.Query {
	if minimum < 2 {
		disjunction := bleve.NewDisjunctionQuery(clauses...)
		disjunction.SetMin(float64(minimum))

		return disjunction
	}

	return rarestTermsMinimumMatchQuery{clauses: clauses, minimum: minimum}
}

func (query rarestTermsMinimumMatchQuery) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	clauses := make([]search.Searcher, 0, len(query.clauses))
	for _, clause := range query.clauses {
		opened, err := clause.Searcher(ctx, reader, indexMapping, options)
		if err != nil {
			for _, openedClause := range clauses {
				_ = openedClause.Close()
			}

			return nil, fmt.Errorf("open minimum-match term clause: %w", err)
		}
		clauses = append(clauses, opened)
	}

	return newRarestTermsMinimumMatchSearcher(clauses, query.minimum, options), nil
}
