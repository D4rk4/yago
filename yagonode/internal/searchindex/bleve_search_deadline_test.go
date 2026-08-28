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

type bleveSearchDeadlineQueryProbe struct {
	searcher search.Searcher
	err      error
	hook     func()
	reader   bleveindex.IndexReader
	calls    int
}

type bleveSearchDeadlineValidatableQueryProbe struct {
	*bleveSearchDeadlineQueryProbe
	err   error
	calls int
}

func (probe *bleveSearchDeadlineValidatableQueryProbe) Validate() error {
	probe.calls++

	return probe.err
}

func (probe *bleveSearchDeadlineQueryProbe) Searcher(
	_ context.Context,
	reader bleveindex.IndexReader,
	_ mapping.IndexMapping,
	_ search.SearcherOptions,
) (search.Searcher, error) {
	probe.calls++
	probe.reader = reader
	if probe.hook != nil {
		probe.hook()
	}

	return probe.searcher, probe.err
}

type bleveSearchDeadlineIndexReaderProbe struct {
	bleveindex.IndexReader
	opened             bleveindex.TermFieldReader
	err                error
	hook               func()
	context            context.Context
	term               string
	field              string
	includeFrequency   bool
	includeNorm        bool
	includeTermVectors bool
	calls              int
}

func (probe *bleveSearchDeadlineIndexReaderProbe) TermFieldReader(
	ctx context.Context,
	term []byte,
	field string,
	includeFrequency bool,
	includeNorm bool,
	includeTermVectors bool,
) (bleveindex.TermFieldReader, error) {
	probe.calls++
	probe.context = ctx
	probe.term = string(term)
	probe.field = field
	probe.includeFrequency = includeFrequency
	probe.includeNorm = includeNorm
	probe.includeTermVectors = includeTermVectors
	if probe.hook != nil {
		probe.hook()
	}

	return probe.opened, probe.err
}

type bleveSearchDeadlineTermReaderProbe struct {
	nextDocument    *bleveindex.TermFieldDoc
	nextErr         error
	nextHook        func()
	nextCalls       int
	advanceDocument *bleveindex.TermFieldDoc
	advanceErr      error
	advanceHook     func()
	advanceID       bleveindex.IndexInternalID
	advanceCalls    int
	count           uint64
	countCalls      int
	closeErr        error
	closeCalls      int
	size            int
}

func (probe *bleveSearchDeadlineTermReaderProbe) Next(
	*bleveindex.TermFieldDoc,
) (*bleveindex.TermFieldDoc, error) {
	probe.nextCalls++
	if probe.nextHook != nil {
		probe.nextHook()
	}

	return probe.nextDocument, probe.nextErr
}

func (probe *bleveSearchDeadlineTermReaderProbe) Advance(
	identifier bleveindex.IndexInternalID,
	_ *bleveindex.TermFieldDoc,
) (*bleveindex.TermFieldDoc, error) {
	probe.advanceCalls++
	probe.advanceID = append(probe.advanceID[:0], identifier...)
	if probe.advanceHook != nil {
		probe.advanceHook()
	}

	return probe.advanceDocument, probe.advanceErr
}

func (probe *bleveSearchDeadlineTermReaderProbe) Count() uint64 {
	probe.countCalls++

	return probe.count
}

func (probe *bleveSearchDeadlineTermReaderProbe) Close() error {
	probe.closeCalls++

	return probe.closeErr
}

func (probe *bleveSearchDeadlineTermReaderProbe) Size() int {
	return probe.size
}

type bleveSearchDeadlineFuzzyReaderProbe struct {
	*bleveSearchDeadlineIndexReaderProbe
	dictionary bleveindex.FieldDict
	automaton  bleveindex.FuzzyAutomaton
	plainErr   error
	autoErr    error
	plainCalls int
	autoCalls  int
}

func (probe *bleveSearchDeadlineFuzzyReaderProbe) FieldDictFuzzy(
	string,
	string,
	int,
	string,
) (bleveindex.FieldDict, error) {
	probe.plainCalls++

	return probe.dictionary, probe.plainErr
}

func (probe *bleveSearchDeadlineFuzzyReaderProbe) FieldDictFuzzyAutomaton(
	string,
	string,
	int,
	string,
) (bleveindex.FieldDict, bleveindex.FuzzyAutomaton, error) {
	probe.autoCalls++

	return probe.dictionary, probe.automaton, probe.autoErr
}

type bleveSearchDeadlineBM25ReaderProbe struct {
	*bleveSearchDeadlineIndexReaderProbe
	cardinality int
	err         error
	calls       int
}

func (probe *bleveSearchDeadlineBM25ReaderProbe) FieldCardinality(
	string,
) (int, error) {
	probe.calls++

	return probe.cardinality, probe.err
}

type bleveSearchDeadlineFuzzyBM25ReaderProbe struct {
	*bleveSearchDeadlineFuzzyReaderProbe
	cardinality      int
	cardinalityErr   error
	cardinalityCalls int
}

func (probe *bleveSearchDeadlineFuzzyBM25ReaderProbe) FieldCardinality(
	string,
) (int, error) {
	probe.cardinalityCalls++

	return probe.cardinality, probe.cardinalityErr
}

func TestBleveSearchDeadlineRefusesCanceledQueryBeforeOpening(t *testing.T) {
	cause := errors.New("query canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	probe := &bleveSearchDeadlineQueryProbe{}
	_, err := withBleveSearchDeadline(SearchRequest{}, probe).Searcher(
		ctx,
		&bleveSearchDeadlineIndexReaderProbe{},
		mapping.NewIndexMapping(),
		search.SearcherOptions{},
	)
	if !errors.Is(err, cause) || probe.calls != 0 {
		t.Fatalf("error=%v calls=%d", err, probe.calls)
	}
}

func TestBleveSearchDeadlinePreservesQueryValidation(t *testing.T) {
	plain := withBleveSearchDeadline(SearchRequest{}, &bleveSearchDeadlineQueryProbe{})
	validatable, ok := plain.(blevequery.ValidatableQuery)
	if !ok {
		t.Fatal("deadline query validation missing")
	}
	if err := validatable.Validate(); err != nil {
		t.Fatal(err)
	}
	validationErr := errors.New("query invalid")
	source := &bleveSearchDeadlineValidatableQueryProbe{
		bleveSearchDeadlineQueryProbe: &bleveSearchDeadlineQueryProbe{},
		err:                           validationErr,
	}
	validatable = withBleveSearchDeadline(SearchRequest{}, source).(blevequery.ValidatableQuery)
	if err := validatable.Validate(); !errors.Is(err, validationErr) || source.calls != 1 {
		t.Fatalf("validation calls=%d error=%v", source.calls, err)
	}
	source.err = nil
	if err := validatable.Validate(); err != nil || source.calls != 2 {
		t.Fatalf("valid calls=%d error=%v", source.calls, err)
	}
}

func TestBleveSearchDeadlinePreservesLiveSearcher(t *testing.T) {
	searcher := newBleveLexicalCandidateSearcherProbe()
	probe := &bleveSearchDeadlineQueryProbe{searcher: searcher}
	opened, err := withBleveSearchDeadline(SearchRequest{}, probe).Searcher(
		t.Context(),
		&bleveSearchDeadlineIndexReaderProbe{},
		mapping.NewIndexMapping(),
		search.SearcherOptions{},
	)
	if err != nil || opened != searcher || probe.calls != 1 || searcher.closed {
		t.Fatalf(
			"searcher=%T calls=%d closed=%t error=%v",
			opened,
			probe.calls,
			searcher.closed,
			err,
		)
	}
	if _, ok := probe.reader.(bleveSearchDeadlineReader); !ok {
		t.Fatalf("reader=%T", probe.reader)
	}
}

func TestBleveSearchDeadlineClosesSearcherCanceledWhileOpening(t *testing.T) {
	for _, closeErr := range []error{nil, errors.New("search close failed")} {
		cause := errors.New("canceled while opening")
		ctx, cancel := context.WithCancelCause(t.Context())
		searcher := newBleveLexicalCandidateSearcherProbe()
		searcher.closeErr = closeErr
		probe := &bleveSearchDeadlineQueryProbe{
			searcher: searcher,
			hook:     func() { cancel(cause) },
		}
		_, err := withBleveSearchDeadline(SearchRequest{}, probe).Searcher(
			ctx,
			&bleveSearchDeadlineIndexReaderProbe{},
			mapping.NewIndexMapping(),
			search.SearcherOptions{},
		)
		if !errors.Is(err, cause) || !searcher.closed ||
			(closeErr != nil && !errors.Is(err, closeErr)) {
			t.Fatalf("close error=%v closed=%t error=%v", closeErr, searcher.closed, err)
		}
	}
}

func TestBleveSearchDeadlineClosesSearcherReturnedWithFailure(t *testing.T) {
	openErr := errors.New("search open failed")
	for _, closeErr := range []error{nil, errors.New("failed search close failed")} {
		searcher := newBleveLexicalCandidateSearcherProbe()
		searcher.closeErr = closeErr
		probe := &bleveSearchDeadlineQueryProbe{searcher: searcher, err: openErr}
		_, err := withBleveSearchDeadline(SearchRequest{}, probe).Searcher(
			t.Context(),
			&bleveSearchDeadlineIndexReaderProbe{},
			mapping.NewIndexMapping(),
			search.SearcherOptions{},
		)
		if !errors.Is(err, openErr) || !searcher.closed ||
			(closeErr != nil && !errors.Is(err, closeErr)) {
			t.Fatalf("close error=%v closed=%t error=%v", closeErr, searcher.closed, err)
		}
	}
	probe := &bleveSearchDeadlineQueryProbe{err: openErr}
	if _, err := withBleveSearchDeadline(SearchRequest{}, probe).Searcher(
		t.Context(),
		&bleveSearchDeadlineIndexReaderProbe{},
		mapping.NewIndexMapping(),
		search.SearcherOptions{},
	); !errors.Is(err, openErr) {
		t.Fatalf("nil searcher error=%v", err)
	}
}

func TestBleveSearchDeadlineRefusesCanceledTermReaderBeforeOpening(t *testing.T) {
	cause := errors.New("term open canceled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	probe := &bleveSearchDeadlineIndexReaderProbe{}
	reader := newBleveSearchDeadlineReader(ctx, probe)
	_, err := reader.TermFieldReader(ctx, []byte("term"), "body", true, true, true)
	if !errors.Is(err, cause) || probe.calls != 0 {
		t.Fatalf("error=%v calls=%d", err, probe.calls)
	}
}

func TestBleveSearchDeadlineOpensBoundedTermReader(t *testing.T) {
	termReader := &bleveSearchDeadlineTermReaderProbe{}
	probe := &bleveSearchDeadlineIndexReaderProbe{opened: termReader}
	ctx := t.Context()
	reader := newBleveSearchDeadlineReader(ctx, probe)
	opened, err := reader.TermFieldReader(ctx, []byte("term"), "body", true, false, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := opened.(bleveSearchDeadlineTermReader); !ok || probe.calls != 1 ||
		probe.context != ctx || probe.term != "term" || probe.field != "body" ||
		!probe.includeFrequency || probe.includeNorm || !probe.includeTermVectors {
		t.Fatalf("reader=%T probe=%+v", opened, probe)
	}
}

func TestBleveSearchDeadlineClosesTermReaderCanceledWhileOpening(t *testing.T) {
	for _, closeErr := range []error{nil, errors.New("term close failed")} {
		cause := errors.New("canceled during term open")
		ctx, cancel := context.WithCancelCause(t.Context())
		termReader := &bleveSearchDeadlineTermReaderProbe{closeErr: closeErr}
		probe := &bleveSearchDeadlineIndexReaderProbe{
			opened: termReader,
			hook:   func() { cancel(cause) },
		}
		_, err := newBleveSearchDeadlineReader(ctx, probe).TermFieldReader(
			ctx, []byte("term"), "body", false, false, false,
		)
		if !errors.Is(err, cause) || termReader.closeCalls != 1 ||
			(closeErr != nil && !errors.Is(err, closeErr)) {
			t.Fatalf("close error=%v calls=%d error=%v", closeErr, termReader.closeCalls, err)
		}
	}
}

func TestBleveSearchDeadlineClosesTermReaderReturnedWithFailure(t *testing.T) {
	openErr := errors.New("term open failed")
	for _, closeErr := range []error{nil, errors.New("failed term close failed")} {
		termReader := &bleveSearchDeadlineTermReaderProbe{closeErr: closeErr}
		probe := &bleveSearchDeadlineIndexReaderProbe{opened: termReader, err: openErr}
		_, err := newBleveSearchDeadlineReader(t.Context(), probe).TermFieldReader(
			t.Context(), []byte("term"), "body", false, false, false,
		)
		if !errors.Is(err, openErr) || termReader.closeCalls != 1 ||
			(closeErr != nil && !errors.Is(err, closeErr)) {
			t.Fatalf("close error=%v calls=%d error=%v", closeErr, termReader.closeCalls, err)
		}
	}
	probe := &bleveSearchDeadlineIndexReaderProbe{err: openErr}
	if _, err := newBleveSearchDeadlineReader(t.Context(), probe).TermFieldReader(
		t.Context(), []byte("term"), "body", false, false, false,
	); !errors.Is(err, openErr) {
		t.Fatalf("nil term reader error=%v", err)
	}
}
