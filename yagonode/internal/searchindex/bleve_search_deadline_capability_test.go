package searchindex

import (
	"errors"
	"testing"

	bleveindex "github.com/blevesearch/bleve_index_api"
)

func TestBleveSearchDeadlineReaderPreservesNoOptionalCapabilities(t *testing.T) {
	reader := newBleveSearchDeadlineReader(
		t.Context(),
		&bleveSearchDeadlineIndexReaderProbe{},
	)
	if _, ok := reader.(bleveindex.IndexReaderFuzzy); ok {
		t.Fatal("plain reader gained fuzzy capability")
	}
	if _, ok := reader.(bleveindex.BM25Reader); ok {
		t.Fatal("plain reader gained BM25 capability")
	}
}

func TestBleveSearchDeadlineReaderPreservesFuzzyCapability(t *testing.T) {
	dictionary := &bleveFuzzySearchDeadlineDictionaryProbe{}
	source := &bleveSearchDeadlineFuzzyReaderProbe{
		bleveSearchDeadlineIndexReaderProbe: &bleveSearchDeadlineIndexReaderProbe{},
		dictionary:                          dictionary,
	}
	reader := newBleveSearchDeadlineReader(t.Context(), source)
	fuzzy, ok := reader.(bleveindex.IndexReaderFuzzy)
	if !ok {
		t.Fatal("fuzzy capability missing")
	}
	if _, ok := reader.(bleveindex.BM25Reader); ok {
		t.Fatal("fuzzy reader gained BM25 capability")
	}
	plain, err := fuzzy.FieldDictFuzzy("body", "term", 1, "t")
	if err != nil || plain != dictionary || source.plainCalls != 1 {
		t.Fatalf("plain=%T calls=%d error=%v", plain, source.plainCalls, err)
	}
	automatonDictionary, automaton, err := fuzzy.FieldDictFuzzyAutomaton(
		"body",
		"term",
		1,
		"t",
	)
	if err != nil || automatonDictionary != dictionary || automaton != nil ||
		source.autoCalls != 1 {
		t.Fatalf(
			"automaton dictionary=%T automaton=%T calls=%d error=%v",
			automatonDictionary,
			automaton,
			source.autoCalls,
			err,
		)
	}
	plainErr := errors.New("plain fuzzy failed")
	source.plainErr = plainErr
	if opened, err := fuzzy.FieldDictFuzzy("body", "term", 1, "t"); opened != nil ||
		!errors.Is(err, plainErr) {
		t.Fatalf("failed plain=%v error=%v", opened, err)
	}
	automatonErr := errors.New("automaton fuzzy failed")
	source.autoErr = automatonErr
	if opened, openedAutomaton, err := fuzzy.FieldDictFuzzyAutomaton(
		"body",
		"term",
		1,
		"t",
	); opened != nil || openedAutomaton != nil || !errors.Is(err, automatonErr) {
		t.Fatalf(
			"failed automaton dictionary=%v automaton=%v error=%v",
			opened,
			openedAutomaton,
			err,
		)
	}
}

func TestBleveSearchDeadlineReaderPreservesBM25Capability(t *testing.T) {
	source := &bleveSearchDeadlineBM25ReaderProbe{
		bleveSearchDeadlineIndexReaderProbe: &bleveSearchDeadlineIndexReaderProbe{},
		cardinality:                         23,
	}
	reader := newBleveSearchDeadlineReader(t.Context(), source)
	bm25, ok := reader.(bleveindex.BM25Reader)
	if !ok {
		t.Fatal("BM25 capability missing")
	}
	if _, ok := reader.(bleveindex.IndexReaderFuzzy); ok {
		t.Fatal("BM25 reader gained fuzzy capability")
	}
	cardinality, err := bm25.FieldCardinality("body")
	if err != nil || cardinality != 23 || source.calls != 1 {
		t.Fatalf("cardinality=%d calls=%d error=%v", cardinality, source.calls, err)
	}
	cardinalityErr := errors.New("cardinality failed")
	source.err = cardinalityErr
	if cardinality, err := bm25.FieldCardinality("body"); cardinality != 0 ||
		!errors.Is(err, cardinalityErr) {
		t.Fatalf("failed cardinality=%d error=%v", cardinality, err)
	}
}

func TestBleveSearchDeadlineReaderPreservesFuzzyBM25Capabilities(t *testing.T) {
	source := &bleveSearchDeadlineFuzzyBM25ReaderProbe{
		bleveSearchDeadlineFuzzyReaderProbe: &bleveSearchDeadlineFuzzyReaderProbe{
			bleveSearchDeadlineIndexReaderProbe: &bleveSearchDeadlineIndexReaderProbe{},
			dictionary:                          &bleveFuzzySearchDeadlineDictionaryProbe{},
		},
		cardinality: 29,
	}
	reader := newBleveSearchDeadlineReader(t.Context(), source)
	if _, ok := reader.(bleveindex.IndexReaderFuzzy); !ok {
		t.Fatal("combined fuzzy capability missing")
	}
	bm25, ok := reader.(bleveindex.BM25Reader)
	if !ok {
		t.Fatal("combined BM25 capability missing")
	}
	cardinality, err := bm25.FieldCardinality("body")
	if err != nil || cardinality != 29 || source.cardinalityCalls != 1 {
		t.Fatalf(
			"cardinality=%d calls=%d error=%v",
			cardinality,
			source.cardinalityCalls,
			err,
		)
	}
	cardinalityErr := errors.New("combined cardinality failed")
	source.cardinalityErr = cardinalityErr
	if cardinality, err := bm25.FieldCardinality("body"); cardinality != 0 ||
		!errors.Is(err, cardinalityErr) {
		t.Fatalf("failed cardinality=%d error=%v", cardinality, err)
	}
}
