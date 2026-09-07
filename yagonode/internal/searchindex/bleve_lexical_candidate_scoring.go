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

type bleveLexicalCandidateScoringQuery struct {
	*blevequery.ConjunctionQuery
}

func (query bleveLexicalCandidateScoringQuery) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	candidates, err := query.Conjuncts[1].Searcher(ctx, reader, indexMapping, options)
	if err != nil {
		return nil, fmt.Errorf("open lexical candidates: %w", err)
	}
	if candidates.Count() == 0 {
		return candidates, nil
	}
	ranked, err := query.Conjuncts[0].Searcher(ctx, reader, indexMapping, options)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open candidate scoring: %w", err), candidates.Close())
	}
	combined, err := searcher.NewConjunctionSearcher(
		ctx, reader, []search.Searcher{ranked, candidates}, options,
	)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("combine candidate scoring: %w", err), ranked.Close(), candidates.Close(),
		)
	}

	return combined, nil
}
