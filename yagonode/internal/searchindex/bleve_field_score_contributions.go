package searchindex

import (
	"context"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type bleveFieldScoreContributionsKey struct{}

type bleveFieldScoreMatchQuery struct {
	*blevequery.MatchQuery
}

type bleveFieldScoreTermQuery struct {
	*blevequery.TermQuery
}

type bleveFieldScoreLeaf struct {
	scored       blevequery.Query
	contribution func() string
}

type bleveFieldScoreSearcher struct {
	search.Searcher
	contribution string
}

func bleveFieldScoreSearchContext(ctx context.Context, req SearchRequest) context.Context {
	if !req.IncludeFieldScores || req.Explain {
		return ctx
	}

	return context.WithValue(ctx, bleveFieldScoreContributionsKey{}, true)
}

func (query bleveFieldScoreMatchQuery) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	return bleveFieldScoreLeaf{
		scored: query.MatchQuery,
		contribution: func() string {
			return bleveFieldScoreContribution(
				query.FieldVal,
				analyzedMatchText(query.MatchQuery, indexMapping),
				query.Boost(),
			)
		},
	}.open(ctx, reader, indexMapping, options)
}

func (query bleveFieldScoreTermQuery) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	return bleveFieldScoreLeaf{
		scored: query.TermQuery,
		contribution: func() string {
			return bleveFieldScoreContribution(query.FieldVal, query.Term, query.Boost())
		},
	}.open(ctx, reader, indexMapping, options)
}

func (leaf bleveFieldScoreLeaf) open(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	requested, _ := ctx.Value(bleveFieldScoreContributionsKey{}).(bool)
	contributionsOnly := requested && options.Explain
	options.Explain = options.Explain && !contributionsOnly
	opened, err := leaf.scored.Searcher(ctx, reader, indexMapping, options)
	if err != nil {
		return nil, fmt.Errorf("open field score leaf: %w", err)
	}
	if !contributionsOnly {
		return opened, nil
	}

	return bleveFieldScoreSearcher{Searcher: opened, contribution: leaf.contribution()}, nil
}

func (s bleveFieldScoreSearcher) Next(ctx *search.SearchContext) (*search.DocumentMatch, error) {
	return s.contributed(s.Searcher.Next(ctx))
}

func (s bleveFieldScoreSearcher) Advance(
	ctx *search.SearchContext,
	id bleveindex.IndexInternalID,
) (*search.DocumentMatch, error) {
	return s.contributed(s.Searcher.Advance(ctx, id))
}

func (s bleveFieldScoreSearcher) contributed(
	match *search.DocumentMatch,
	err error,
) (*search.DocumentMatch, error) {
	if err != nil {
		return nil, fmt.Errorf("read field score match: %w", err)
	}
	if match != nil {
		match.Expl = &search.Explanation{Value: match.Score, Message: s.contribution}
	}

	return match, nil
}

func bleveFieldScoreContribution(field string, term string, boost float64) string {
	return fmt.Sprintf("weight(%s:%s^%f in ", field, term, boost)
}

func analyzedMatchText(query *blevequery.MatchQuery, indexMapping mapping.IndexMapping) string {
	analyzerName := query.Analyzer
	if analyzerName == "" {
		analyzerName = indexMapping.AnalyzerNameForPath(query.FieldVal)
	}
	tokens := indexMapping.AnalyzerNamed(analyzerName).Analyze([]byte(query.Match))
	terms := make([]string, 0, len(tokens))
	for _, token := range tokens {
		terms = append(terms, string(token.Term))
	}

	return strings.Join(terms, " ")
}
