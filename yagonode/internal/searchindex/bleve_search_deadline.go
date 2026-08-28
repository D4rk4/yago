package searchindex

import (
	"context"
	"errors"
	"fmt"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type bleveSearchDeadlineQuery struct {
	inner      blevequery.Query
	validation blevequery.Query
}

type bleveSearchDeadlineSearcherOpening struct {
	opened search.Searcher
	err    error
}

type bleveSearchDeadlineTermReaderOpening struct {
	opened bleveindex.TermFieldReader
	err    error
}

func withBleveSearchDeadline(
	req SearchRequest,
	query blevequery.Query,
) blevequery.Query {
	return bleveSearchDeadlineQuery{
		inner:      withBleveFuzzySearchDeadline(req, query),
		validation: query,
	}
}

func (query bleveSearchDeadlineQuery) Validate() error {
	validatable, ok := query.validation.(blevequery.ValidatableQuery)
	if !ok {
		return nil
	}
	if err := validatable.Validate(); err != nil {
		return fmt.Errorf("validate search: %w", err)
	}

	return nil
}

func (query bleveSearchDeadlineQuery) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	if cause := context.Cause(ctx); cause != nil {
		return nil, fmt.Errorf("search context: %w", cause)
	}
	opening := newBleveSearchDeadlineSearcherOpening(query.inner.Searcher(
		ctx,
		newBleveSearchDeadlineReader(ctx, reader),
		indexMapping,
		options,
	))
	if opening.err != nil {
		openErr := fmt.Errorf("open search: %w", opening.err)
		if opening.opened == nil {
			return nil, openErr
		}
		if closeErr := opening.opened.Close(); closeErr != nil {
			return nil, errors.Join(
				openErr,
				fmt.Errorf("close failed search: %w", closeErr),
			)
		}

		return nil, openErr
	}
	if cause := context.Cause(ctx); cause != nil {
		closeErr := opening.opened.Close()
		return nil, bleveSearchDeadlineClosureError(
			"search context",
			cause,
			"close canceled search",
			closeErr,
		)
	}

	return opening.opened, nil
}

type bleveSearchDeadlineReader struct {
	bleveindex.IndexReader
	ctx context.Context
}

func newBleveSearchDeadlineReader(
	ctx context.Context,
	reader bleveindex.IndexReader,
) bleveindex.IndexReader {
	bounded := bleveSearchDeadlineReader{IndexReader: reader, ctx: ctx}
	fuzzy, fuzzyAvailable := reader.(bleveindex.IndexReaderFuzzy)
	bm25, bm25Available := reader.(bleveindex.BM25Reader)
	switch {
	case fuzzyAvailable && bm25Available:
		return bleveSearchDeadlineAutomatonBM25Reader{
			bleveSearchDeadlineAutomatonReader: bleveSearchDeadlineAutomatonReader{
				bleveSearchDeadlineReader: bounded,
				fuzzy:                     fuzzy,
			},
			bm25: bm25,
		}
	case fuzzyAvailable:
		return bleveSearchDeadlineAutomatonReader{
			bleveSearchDeadlineReader: bounded,
			fuzzy:                     fuzzy,
		}
	case bm25Available:
		return bleveSearchDeadlineBM25Reader{
			bleveSearchDeadlineReader: bounded,
			bm25:                      bm25,
		}
	default:
		return bounded
	}
}

func (reader bleveSearchDeadlineReader) TermFieldReader(
	ctx context.Context,
	term []byte,
	field string,
	includeFrequency bool,
	includeNorm bool,
	includeTermVectors bool,
) (bleveindex.TermFieldReader, error) {
	if cause := context.Cause(reader.ctx); cause != nil {
		return nil, fmt.Errorf("term reader context: %w", cause)
	}
	opening := newBleveSearchDeadlineTermReaderOpening(reader.IndexReader.TermFieldReader(
		ctx,
		term,
		field,
		includeFrequency,
		includeNorm,
		includeTermVectors,
	))
	if opening.err != nil {
		openErr := fmt.Errorf("open term reader: %w", opening.err)
		if opening.opened == nil {
			return nil, openErr
		}
		if closeErr := opening.opened.Close(); closeErr != nil {
			return nil, errors.Join(
				openErr,
				fmt.Errorf("close failed term reader: %w", closeErr),
			)
		}

		return nil, openErr
	}
	if cause := context.Cause(reader.ctx); cause != nil {
		closeErr := opening.opened.Close()
		return nil, bleveSearchDeadlineClosureError(
			"term reader context",
			cause,
			"close canceled term reader",
			closeErr,
		)
	}

	return newBleveSearchDeadlineTermReader(reader.ctx, opening.opened), nil
}

func newBleveSearchDeadlineSearcherOpening(
	opened search.Searcher,
	err error,
) bleveSearchDeadlineSearcherOpening {
	return bleveSearchDeadlineSearcherOpening{opened: opened, err: err}
}

func newBleveSearchDeadlineTermReaderOpening(
	opened bleveindex.TermFieldReader,
	err error,
) bleveSearchDeadlineTermReaderOpening {
	return bleveSearchDeadlineTermReaderOpening{opened: opened, err: err}
}

type bleveSearchDeadlineAutomatonReader struct {
	bleveSearchDeadlineReader
	fuzzy bleveindex.IndexReaderFuzzy
}

func (reader bleveSearchDeadlineAutomatonReader) FieldDictFuzzy(
	field string,
	term string,
	fuzziness int,
	prefix string,
) (bleveindex.FieldDict, error) {
	dictionary, err := reader.fuzzy.FieldDictFuzzy(field, term, fuzziness, prefix)
	if err != nil {
		return nil, fmt.Errorf("open search fuzzy dictionary: %w", err)
	}

	return dictionary, nil
}

func (reader bleveSearchDeadlineAutomatonReader) FieldDictFuzzyAutomaton(
	field string,
	term string,
	fuzziness int,
	prefix string,
) (bleveindex.FieldDict, bleveindex.FuzzyAutomaton, error) {
	dictionary, automaton, err := reader.fuzzy.FieldDictFuzzyAutomaton(
		field,
		term,
		fuzziness,
		prefix,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("open search fuzzy automaton: %w", err)
	}

	return dictionary, automaton, nil
}

type bleveSearchDeadlineBM25Reader struct {
	bleveSearchDeadlineReader
	bm25 bleveindex.BM25Reader
}

func (reader bleveSearchDeadlineBM25Reader) FieldCardinality(
	field string,
) (int, error) {
	return bleveSearchDeadlineFieldCardinality(reader.bm25, field)
}

type bleveSearchDeadlineAutomatonBM25Reader struct {
	bleveSearchDeadlineAutomatonReader
	bm25 bleveindex.BM25Reader
}

func (reader bleveSearchDeadlineAutomatonBM25Reader) FieldCardinality(
	field string,
) (int, error) {
	return bleveSearchDeadlineFieldCardinality(reader.bm25, field)
}

func bleveSearchDeadlineFieldCardinality(
	reader bleveindex.BM25Reader,
	field string,
) (int, error) {
	cardinality, err := reader.FieldCardinality(field)
	if err != nil {
		return 0, fmt.Errorf("read search field cardinality: %w", err)
	}

	return cardinality, nil
}

func bleveSearchDeadlineClosureError(
	contextOperation string,
	cause error,
	closeOperation string,
	closeErr error,
) error {
	contextErr := fmt.Errorf("%s: %w", contextOperation, cause)
	if closeErr == nil {
		return contextErr
	}

	return errors.Join(contextErr, fmt.Errorf("%s: %w", closeOperation, closeErr))
}

var (
	_ blevequery.Query            = bleveSearchDeadlineQuery{}
	_ blevequery.ValidatableQuery = bleveSearchDeadlineQuery{}
	_ bleveindex.IndexReaderFuzzy = bleveSearchDeadlineAutomatonReader{}
	_ bleveindex.BM25Reader       = bleveSearchDeadlineBM25Reader{}
	_ bleveindex.BM25Reader       = bleveSearchDeadlineAutomatonBM25Reader{}
)
