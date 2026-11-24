package searchindex

import (
	"context"
	"errors"
	"fmt"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/blevesearch/bleve/v2/search/searcher"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type fuzzyCandidateRequirements []blevequery.Query

func (requirements fuzzyCandidateRequirements) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	opened := make([]search.Searcher, 0, len(requirements))
	for _, requirement := range requirements {
		clause, err := requirement.Searcher(ctx, reader, indexMapping, options)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("open fuzzy candidate requirement: %w", err),
				closeFuzzyRequirements(opened),
			)
		}
		opened = append(opened, clause)
		if clause.Count() == 0 {
			if err := errors.Join(closeFuzzyRequirements(opened), context.Cause(ctx)); err != nil {
				return nil, fmt.Errorf("finish empty fuzzy candidate search: %w", err)
			}
			return newBleveLexicalCandidateInternalSearcher(nil, options), nil
		}
	}
	combined, err := searcher.NewConjunctionSearcher(ctx, reader, opened, options)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("combine fuzzy candidate requirements: %w", err),
			closeFuzzyRequirements(opened),
		)
	}
	return combined, nil
}

func closeFuzzyRequirements(opened []search.Searcher) error {
	var err error
	for _, clause := range opened {
		err = errors.Join(err, clause.Close())
	}
	return err
}
