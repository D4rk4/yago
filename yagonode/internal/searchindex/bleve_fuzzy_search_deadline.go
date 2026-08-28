package searchindex

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type bleveFuzzySearchDeadlineQuery struct {
	inner blevequery.Query
}

func withBleveFuzzySearchDeadline(
	req SearchRequest,
	query blevequery.Query,
) blevequery.Query {
	if !req.Fuzzy {
		return query
	}

	return bleveFuzzySearchDeadlineQuery{inner: query}
}

func (query bleveFuzzySearchDeadlineQuery) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	if cause := context.Cause(ctx); cause != nil {
		return nil, fmt.Errorf("fuzzy search context: %w", cause)
	}
	opened, err := query.inner.Searcher(
		ctx,
		newBleveFuzzySearchDeadlineReader(ctx, reader),
		indexMapping,
		options,
	)
	if err != nil {
		return nil, fmt.Errorf("open fuzzy search: %w", err)
	}

	return opened, nil
}

type bleveFuzzySearchDeadlineReader struct {
	bleveindex.IndexReader
	ctx context.Context
}

func newBleveFuzzySearchDeadlineReader(
	ctx context.Context,
	reader bleveindex.IndexReader,
) bleveindex.IndexReader {
	bounded := bleveFuzzySearchDeadlineReader{IndexReader: reader, ctx: ctx}
	fuzzy, fuzzyAvailable := reader.(bleveindex.IndexReaderFuzzy)
	bm25, bm25Available := reader.(bleveindex.BM25Reader)
	switch {
	case fuzzyAvailable && bm25Available:
		return bleveFuzzySearchDeadlineAutomatonBM25Reader{
			bleveFuzzySearchDeadlineAutomatonReader: bleveFuzzySearchDeadlineAutomatonReader{
				bleveFuzzySearchDeadlineReader: bounded,
				fuzzy:                          fuzzy,
			},
			bm25: bm25,
		}
	case fuzzyAvailable:
		return bleveFuzzySearchDeadlineAutomatonReader{
			bleveFuzzySearchDeadlineReader: bounded,
			fuzzy:                          fuzzy,
		}
	case bm25Available:
		return bleveFuzzySearchDeadlineBM25Reader{
			bleveFuzzySearchDeadlineReader: bounded,
			bm25:                           bm25,
		}
	default:
		return bounded
	}
}

func (reader bleveFuzzySearchDeadlineReader) FieldDict(
	field string,
) (bleveindex.FieldDict, error) {
	dictionary, err := reader.IndexReader.FieldDict(field)

	return newBleveFuzzySearchDeadlineDictionary(reader.ctx, dictionary, err)
}

func (reader bleveFuzzySearchDeadlineReader) FieldDictPrefix(
	field string,
	prefix []byte,
) (bleveindex.FieldDict, error) {
	dictionary, err := reader.IndexReader.FieldDictPrefix(field, prefix)

	return newBleveFuzzySearchDeadlineDictionary(reader.ctx, dictionary, err)
}

type bleveFuzzySearchDeadlineAutomatonReader struct {
	bleveFuzzySearchDeadlineReader
	fuzzy bleveindex.IndexReaderFuzzy
}

type bleveFuzzySearchDeadlineBM25Reader struct {
	bleveFuzzySearchDeadlineReader
	bm25 bleveindex.BM25Reader
}

func (reader bleveFuzzySearchDeadlineBM25Reader) FieldCardinality(
	field string,
) (int, error) {
	return bleveFuzzySearchDeadlineFieldCardinality(reader.bm25, field)
}

type bleveFuzzySearchDeadlineAutomatonBM25Reader struct {
	bleveFuzzySearchDeadlineAutomatonReader
	bm25 bleveindex.BM25Reader
}

func (reader bleveFuzzySearchDeadlineAutomatonBM25Reader) FieldCardinality(
	field string,
) (int, error) {
	return bleveFuzzySearchDeadlineFieldCardinality(reader.bm25, field)
}

func bleveFuzzySearchDeadlineFieldCardinality(
	reader bleveindex.BM25Reader,
	field string,
) (int, error) {
	cardinality, err := reader.FieldCardinality(field)
	if err != nil {
		return 0, fmt.Errorf("read fuzzy field cardinality: %w", err)
	}

	return cardinality, nil
}

func (reader bleveFuzzySearchDeadlineAutomatonReader) FieldDictFuzzy(
	field string,
	term string,
	fuzziness int,
	prefix string,
) (bleveindex.FieldDict, error) {
	dictionary, err := reader.fuzzy.FieldDictFuzzy(field, term, fuzziness, prefix)

	return newBleveFuzzySearchDeadlineDictionary(reader.ctx, dictionary, err)
}

func (reader bleveFuzzySearchDeadlineAutomatonReader) FieldDictFuzzyAutomaton(
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
	bounded, err := newBleveFuzzySearchDeadlineDictionary(reader.ctx, dictionary, err)
	if err != nil {
		return nil, nil, err
	}

	return bounded, automaton, nil
}

type bleveFuzzySearchDeadlineDictionary struct {
	inner bleveindex.FieldDict
	ctx   context.Context
}

func newBleveFuzzySearchDeadlineDictionary(
	ctx context.Context,
	dictionary bleveindex.FieldDict,
	err error,
) (bleveindex.FieldDict, error) {
	if err != nil {
		return nil, fmt.Errorf("open fuzzy dictionary: %w", err)
	}

	return bleveFuzzySearchDeadlineDictionary{inner: dictionary, ctx: ctx}, nil
}

func (dictionary bleveFuzzySearchDeadlineDictionary) Next() (
	*bleveindex.DictEntry,
	error,
) {
	if cause := context.Cause(dictionary.ctx); cause != nil {
		return nil, fmt.Errorf("fuzzy dictionary context: %w", cause)
	}
	entry, err := dictionary.inner.Next()
	if err != nil {
		return nil, fmt.Errorf("read fuzzy dictionary: %w", err)
	}
	if cause := context.Cause(dictionary.ctx); cause != nil {
		return nil, fmt.Errorf("fuzzy dictionary context: %w", cause)
	}

	return entry, nil
}

func (dictionary bleveFuzzySearchDeadlineDictionary) Close() error {
	if err := dictionary.inner.Close(); err != nil {
		return fmt.Errorf("close fuzzy dictionary: %w", err)
	}

	return nil
}

func (dictionary bleveFuzzySearchDeadlineDictionary) Cardinality() int {
	return dictionary.inner.Cardinality()
}

func (dictionary bleveFuzzySearchDeadlineDictionary) BytesRead() uint64 {
	return dictionary.inner.BytesRead()
}

var (
	_ blevequery.Query            = bleveFuzzySearchDeadlineQuery{}
	_ bleveindex.IndexReaderFuzzy = bleveFuzzySearchDeadlineAutomatonReader{}
	_ bleveindex.BM25Reader       = bleveFuzzySearchDeadlineBM25Reader{}
	_ bleveindex.BM25Reader       = bleveFuzzySearchDeadlineAutomatonBM25Reader{}
	_ bleveindex.FieldDict        = bleveFuzzySearchDeadlineDictionary{}
)
