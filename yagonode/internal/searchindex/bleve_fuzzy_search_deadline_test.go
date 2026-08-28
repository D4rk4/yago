package searchindex

import (
	"context"
	"errors"
	"testing"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type bleveFuzzySearchDeadlineDictionaryProbe struct {
	entries     []*bleveindex.DictEntry
	position    int
	next        func()
	nextErr     error
	closeErr    error
	closed      bool
	cardinality int
	bytesRead   uint64
}

func (probe *bleveFuzzySearchDeadlineDictionaryProbe) Next() (
	*bleveindex.DictEntry,
	error,
) {
	if probe.next != nil {
		probe.next()
	}
	if probe.nextErr != nil {
		return nil, probe.nextErr
	}
	if probe.position >= len(probe.entries) {
		return nil, nil
	}
	entry := probe.entries[probe.position]
	probe.position++

	return entry, nil
}

func (probe *bleveFuzzySearchDeadlineDictionaryProbe) Close() error {
	probe.closed = true

	return probe.closeErr
}

func (probe *bleveFuzzySearchDeadlineDictionaryProbe) Cardinality() int {
	return probe.cardinality
}

func (probe *bleveFuzzySearchDeadlineDictionaryProbe) BytesRead() uint64 {
	return probe.bytesRead
}

type bleveFuzzySearchDeadlineAdvancedReaderProbe struct {
	bleveindex.IndexReader
	dictionary            bleveindex.FieldDict
	automaton             bleveindex.FuzzyAutomaton
	err                   error
	field                 string
	term                  string
	fuzziness             int
	prefix                string
	plainCalls            int
	autoCalls             int
	fieldCardinality      int
	fieldCardinalityErr   error
	fieldCardinalityCalls int
}

func (probe *bleveFuzzySearchDeadlineAdvancedReaderProbe) FieldCardinality(
	string,
) (int, error) {
	probe.fieldCardinalityCalls++

	return probe.fieldCardinality, probe.fieldCardinalityErr
}

func (probe *bleveFuzzySearchDeadlineAdvancedReaderProbe) FieldDictFuzzy(
	field string,
	term string,
	fuzziness int,
	prefix string,
) (bleveindex.FieldDict, error) {
	probe.plainCalls++
	probe.capture(field, term, fuzziness, prefix)

	return probe.dictionary, probe.err
}

func (probe *bleveFuzzySearchDeadlineAdvancedReaderProbe) FieldDictFuzzyAutomaton(
	field string,
	term string,
	fuzziness int,
	prefix string,
) (bleveindex.FieldDict, bleveindex.FuzzyAutomaton, error) {
	probe.autoCalls++
	probe.capture(field, term, fuzziness, prefix)

	return probe.dictionary, probe.automaton, probe.err
}

func (probe *bleveFuzzySearchDeadlineAdvancedReaderProbe) capture(
	field string,
	term string,
	fuzziness int,
	prefix string,
) {
	probe.field = field
	probe.term = term
	probe.fuzziness = fuzziness
	probe.prefix = prefix
}

type bleveFuzzySearchDeadlineFallbackReaderProbe struct {
	bleveindex.IndexReader
	dictionary  bleveindex.FieldDict
	err         error
	field       string
	prefix      string
	allCalls    int
	prefixCalls int
}

func (probe *bleveFuzzySearchDeadlineFallbackReaderProbe) FieldDict(
	field string,
) (bleveindex.FieldDict, error) {
	probe.allCalls++
	probe.field = field

	return probe.dictionary, probe.err
}

func (probe *bleveFuzzySearchDeadlineFallbackReaderProbe) FieldDictPrefix(
	field string,
	prefix []byte,
) (bleveindex.FieldDict, error) {
	probe.prefixCalls++
	probe.field = field
	probe.prefix = string(prefix)

	return probe.dictionary, probe.err
}

type bleveFuzzySearchDeadlineFallbackBM25ReaderProbe struct {
	*bleveFuzzySearchDeadlineFallbackReaderProbe
	fieldCardinality int
	calls            int
}

func (probe *bleveFuzzySearchDeadlineFallbackBM25ReaderProbe) FieldCardinality(
	string,
) (int, error) {
	probe.calls++

	return probe.fieldCardinality, nil
}

type bleveFuzzySearchDeadlineFuzzyOnlyReaderProbe struct {
	bleveindex.IndexReader
	dictionary bleveindex.FieldDict
}

func (probe *bleveFuzzySearchDeadlineFuzzyOnlyReaderProbe) FieldDictFuzzy(
	string,
	string,
	int,
	string,
) (bleveindex.FieldDict, error) {
	return probe.dictionary, nil
}

func (probe *bleveFuzzySearchDeadlineFuzzyOnlyReaderProbe) FieldDictFuzzyAutomaton(
	string,
	string,
	int,
	string,
) (bleveindex.FieldDict, bleveindex.FuzzyAutomaton, error) {
	return probe.dictionary, nil, nil
}

func TestBleveFuzzySearchDeadlineStopsAdvancedDictionaryEnumeration(t *testing.T) {
	cause := errors.New("fuzzy deadline")
	ctx, cancel := context.WithCancelCause(t.Context())
	dictionary := &bleveFuzzySearchDeadlineDictionaryProbe{
		entries: []*bleveindex.DictEntry{{Term: "paleoneurology", EditDistance: 1}},
		next:    func() { cancel(cause) },
	}
	reader := &bleveFuzzySearchDeadlineAdvancedReaderProbe{dictionary: dictionary}
	query := blevequery.NewFuzzyQuery("paleoneurology")
	query.SetField("body")
	query.SetFuzziness(2)
	query.SetPrefix(4)
	_, err := withBleveFuzzySearchDeadline(
		SearchRequest{Fuzzy: true},
		query,
	).Searcher(ctx, reader, mapping.NewIndexMapping(), search.SearcherOptions{})
	if !errors.Is(err, cause) {
		t.Fatalf("searcher error=%v, want=%v", err, cause)
	}
	if dictionary.position != 1 || !dictionary.closed || reader.autoCalls != 1 ||
		reader.plainCalls != 0 || reader.field != "body" ||
		reader.term != "paleoneurology" || reader.fuzziness != 2 || reader.prefix != "pale" {
		t.Fatalf(
			"dictionary position/closed=%d/%t calls=%d/%d request=%s/%s/%d/%s",
			dictionary.position,
			dictionary.closed,
			reader.autoCalls,
			reader.plainCalls,
			reader.field,
			reader.term,
			reader.fuzziness,
			reader.prefix,
		)
	}
}

func TestBleveFuzzySearchDeadlineStopsFallbackDictionaryEnumeration(t *testing.T) {
	cause := errors.New("fallback fuzzy deadline")
	ctx, cancel := context.WithCancelCause(t.Context())
	dictionary := &bleveFuzzySearchDeadlineDictionaryProbe{
		entries: []*bleveindex.DictEntry{{Term: "paleoneurology"}},
		next:    func() { cancel(cause) },
	}
	reader := &bleveFuzzySearchDeadlineFallbackReaderProbe{dictionary: dictionary}
	query := blevequery.NewFuzzyQuery("paleoneurology")
	query.SetField("body")
	query.SetFuzziness(2)
	query.SetPrefix(4)
	_, err := withBleveFuzzySearchDeadline(
		SearchRequest{Fuzzy: true},
		query,
	).Searcher(ctx, reader, mapping.NewIndexMapping(), search.SearcherOptions{})
	if !errors.Is(err, cause) {
		t.Fatalf("searcher error=%v, want=%v", err, cause)
	}
	if dictionary.position != 1 || !dictionary.closed || reader.prefixCalls != 1 ||
		reader.allCalls != 0 || reader.field != "body" || reader.prefix != "pale" {
		t.Fatalf(
			"dictionary position/closed=%d/%t calls=%d/%d request=%s/%s",
			dictionary.position,
			dictionary.closed,
			reader.prefixCalls,
			reader.allCalls,
			reader.field,
			reader.prefix,
		)
	}
	if _, ok := newBleveFuzzySearchDeadlineReader(ctx, reader).(bleveindex.IndexReaderFuzzy); ok {
		t.Fatal("fallback reader gained unsupported fuzzy interface")
	}
}

func TestBleveFuzzySearchDeadlineWrapsFallbackDictionary(t *testing.T) {
	dictionary := &bleveFuzzySearchDeadlineDictionaryProbe{}
	reader := &bleveFuzzySearchDeadlineFallbackReaderProbe{dictionary: dictionary}
	bounded := newBleveFuzzySearchDeadlineReader(t.Context(), reader)
	opened, err := bounded.FieldDict("title")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := opened.(bleveFuzzySearchDeadlineDictionary); !ok ||
		reader.allCalls != 1 || reader.field != "title" {
		t.Fatalf("dictionary=%T calls=%d field=%s", opened, reader.allCalls, reader.field)
	}
}

func TestBleveFuzzySearchDeadlinePreservesReaderCapabilities(t *testing.T) {
	dictionary := &bleveFuzzySearchDeadlineDictionaryProbe{}
	bm25Source := &bleveFuzzySearchDeadlineFallbackBM25ReaderProbe{
		bleveFuzzySearchDeadlineFallbackReaderProbe: &bleveFuzzySearchDeadlineFallbackReaderProbe{
			dictionary: dictionary,
		},
		fieldCardinality: 19,
	}
	bm25Reader := newBleveFuzzySearchDeadlineReader(t.Context(), bm25Source)
	bm25, ok := bm25Reader.(bleveindex.BM25Reader)
	if !ok {
		t.Fatal("fallback BM25 interface missing")
	}
	if cardinality, err := bm25.FieldCardinality(
		"body",
	); err != nil || cardinality != 19 ||
		bm25Source.calls != 1 {
		t.Fatalf("field cardinality=%d calls=%d error=%v", cardinality, bm25Source.calls, err)
	}
	if _, ok := bm25Reader.(bleveindex.IndexReaderFuzzy); ok {
		t.Fatal("fallback BM25 reader gained fuzzy interface")
	}
	fuzzyReader := newBleveFuzzySearchDeadlineReader(
		t.Context(),
		&bleveFuzzySearchDeadlineFuzzyOnlyReaderProbe{dictionary: dictionary},
	)
	if _, ok := fuzzyReader.(bleveindex.IndexReaderFuzzy); !ok {
		t.Fatal("fuzzy-only interface missing")
	}
	if _, ok := fuzzyReader.(bleveindex.BM25Reader); ok {
		t.Fatal("fuzzy-only reader gained BM25 interface")
	}
}

func TestBleveFuzzySearchDeadlineRefusesCanceledQueryBeforeOpeningReader(t *testing.T) {
	cause := errors.New("already canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	dictionary := &bleveFuzzySearchDeadlineDictionaryProbe{}
	reader := &bleveFuzzySearchDeadlineAdvancedReaderProbe{dictionary: dictionary}
	_, err := withBleveFuzzySearchDeadline(
		SearchRequest{Fuzzy: true},
		blevequery.NewFuzzyQuery("term"),
	).Searcher(ctx, reader, mapping.NewIndexMapping(), search.SearcherOptions{})
	if !errors.Is(err, cause) || reader.autoCalls != 0 || reader.plainCalls != 0 ||
		dictionary.closed {
		t.Fatalf(
			"error=%v calls=%d/%d closed=%t",
			err,
			reader.autoCalls,
			reader.plainCalls,
			dictionary.closed,
		)
	}
}

func TestBleveFuzzySearchDeadlineDictionaryRefusesBeforeRead(t *testing.T) {
	cause := errors.New("dictionary canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	calls := 0
	probe := &bleveFuzzySearchDeadlineDictionaryProbe{next: func() { calls++ }}
	dictionary, err := newBleveFuzzySearchDeadlineDictionary(ctx, probe, nil)
	if err != nil {
		t.Fatal(err)
	}
	if entry, err := dictionary.Next(); entry != nil || !errors.Is(err, cause) || calls != 0 {
		t.Fatalf("entry=%v calls=%d error=%v", entry, calls, err)
	}
}

func TestBleveFuzzySearchDeadlineLeavesExactQueryUnchanged(t *testing.T) {
	query := blevequery.NewMatchAllQuery()
	if got := withBleveFuzzySearchDeadline(SearchRequest{}, query); got != query {
		t.Fatalf("exact query changed from %p to %T", query, got)
	}
}

func TestBleveFuzzySearchDeadlineDictionaryPreservesResultsAndFailures(t *testing.T) {
	entry := &bleveindex.DictEntry{Term: "accepted", Count: 7, EditDistance: 1}
	nextErr := errors.New("next failed")
	closeErr := errors.New("close failed")
	probe := &bleveFuzzySearchDeadlineDictionaryProbe{
		entries:     []*bleveindex.DictEntry{entry},
		cardinality: 11,
		bytesRead:   13,
		closeErr:    closeErr,
	}
	dictionary, err := newBleveFuzzySearchDeadlineDictionary(t.Context(), probe, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := dictionary.Next()
	if err != nil || got != entry || dictionary.Cardinality() != 11 ||
		dictionary.BytesRead() != 13 {
		t.Fatalf(
			"entry=%v cardinality=%d bytes=%d error=%v",
			got,
			dictionary.Cardinality(),
			dictionary.BytesRead(),
			err,
		)
	}
	probe.nextErr = nextErr
	if _, err := dictionary.Next(); !errors.Is(err, nextErr) {
		t.Fatalf("next error=%v, want=%v", err, nextErr)
	}
	probe.nextErr = nil
	if got, err := dictionary.Next(); err != nil || got != nil {
		t.Fatalf("exhausted entry=%v error=%v", got, err)
	}
	if err := dictionary.Close(); !errors.Is(err, closeErr) || !probe.closed {
		t.Fatalf("close error=%v closed=%t", err, probe.closed)
	}
	openErr := errors.New("open failed")
	if got, err := newBleveFuzzySearchDeadlineDictionary(
		t.Context(),
		nil,
		openErr,
	); got != nil ||
		!errors.Is(err, openErr) {
		t.Fatalf("open result=%v error=%v", got, err)
	}
}

func TestBleveFuzzySearchDeadlineReaderWrapsBothAdvancedMethods(t *testing.T) {
	probe := &bleveFuzzySearchDeadlineDictionaryProbe{}
	reader := &bleveFuzzySearchDeadlineAdvancedReaderProbe{
		dictionary:       probe,
		fieldCardinality: 17,
	}
	deadlineReader := newBleveFuzzySearchDeadlineReader(
		t.Context(),
		reader,
	)
	bounded, ok := deadlineReader.(bleveindex.IndexReaderFuzzy)
	if !ok {
		t.Fatal("advanced fuzzy interface missing")
	}
	bm25, ok := deadlineReader.(bleveindex.BM25Reader)
	if !ok {
		t.Fatal("BM25 interface missing")
	}
	if cardinality, err := bm25.FieldCardinality("body"); err != nil || cardinality != 17 {
		t.Fatalf("field cardinality=%d error=%v", cardinality, err)
	}
	plain, err := bounded.FieldDictFuzzy("title", "term", 1, "t")
	if err != nil {
		t.Fatal(err)
	}
	automatonDictionary, automaton, err := bounded.FieldDictFuzzyAutomaton(
		"body", "word", 2, "wo",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.(bleveFuzzySearchDeadlineDictionary); !ok {
		t.Fatalf("plain dictionary=%T", plain)
	}
	if _, ok := automatonDictionary.(bleveFuzzySearchDeadlineDictionary); !ok || automaton != nil {
		t.Fatalf("automaton dictionary=%T automaton=%T", automatonDictionary, automaton)
	}
	if reader.plainCalls != 1 || reader.autoCalls != 1 || reader.fieldCardinalityCalls != 1 {
		t.Fatalf(
			"reader calls=%d/%d/%d",
			reader.plainCalls,
			reader.autoCalls,
			reader.fieldCardinalityCalls,
		)
	}
	fieldCardinalityErr := errors.New("field cardinality failed")
	reader.fieldCardinalityErr = fieldCardinalityErr
	if cardinality, err := bm25.FieldCardinality("body"); cardinality != 0 ||
		!errors.Is(err, fieldCardinalityErr) {
		t.Fatalf("failed field cardinality=%d error=%v", cardinality, err)
	}
	reader.err = errors.New("advanced open failed")
	if dictionary, _, err := bounded.FieldDictFuzzyAutomaton(
		"body",
		"word",
		2,
		"wo",
	); dictionary != nil ||
		!errors.Is(err, reader.err) {
		t.Fatalf("failed dictionary=%v error=%v", dictionary, err)
	}
}
