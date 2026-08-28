package searchindex

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/blevesearch/bleve/v2/search/searcher"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

type bleveLexicalCandidateQueryProbe struct {
	searcher search.Searcher
	err      error
	reader   bleveindex.IndexReader
	options  search.SearcherOptions
	calls    int
}

func (probe *bleveLexicalCandidateQueryProbe) Searcher(
	_ context.Context,
	reader bleveindex.IndexReader,
	_ mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	probe.calls++
	probe.reader = reader
	probe.options = options

	return probe.searcher, probe.err
}

type bleveLexicalCandidateSearcherProbe struct {
	identities  []bleveindex.IndexInternalID
	position    int
	nextErrorAt int
	nextErr     error
	closeErr    error
	closed      bool
	cancel      context.CancelCauseFunc
	cancelAfter int
	cancelCause error
}

func newBleveLexicalCandidateSearcherProbe(
	identities ...bleveindex.IndexInternalID,
) *bleveLexicalCandidateSearcherProbe {
	return &bleveLexicalCandidateSearcherProbe{
		identities:  identities,
		nextErrorAt: -1,
		cancelAfter: -1,
	}
}

func (probe *bleveLexicalCandidateSearcherProbe) Next(
	ctx *search.SearchContext,
) (*search.DocumentMatch, error) {
	if probe.position == probe.nextErrorAt {
		return nil, probe.nextErr
	}
	if probe.position >= len(probe.identities) {
		return nil, nil
	}
	match := ctx.DocumentMatchPool.Get()
	match.IndexInternalID = bleveindex.NewIndexInternalIDFrom(
		match.IndexInternalID,
		probe.identities[probe.position],
	)
	probe.position++
	if probe.cancel != nil && probe.position == probe.cancelAfter {
		probe.cancel(probe.cancelCause)
	}

	return match, nil
}

func (probe *bleveLexicalCandidateSearcherProbe) Advance(
	ctx *search.SearchContext,
	identity bleveindex.IndexInternalID,
) (*search.DocumentMatch, error) {
	for probe.position < len(probe.identities) &&
		probe.identities[probe.position].Compare(identity) < 0 {
		probe.position++
	}

	return probe.Next(ctx)
}

func (probe *bleveLexicalCandidateSearcherProbe) Close() error {
	probe.closed = true

	return probe.closeErr
}

func (*bleveLexicalCandidateSearcherProbe) Weight() float64 {
	return 0
}

func (*bleveLexicalCandidateSearcherProbe) SetQueryNorm(float64) {}

func (probe *bleveLexicalCandidateSearcherProbe) Count() uint64 {
	return uint64(len(probe.identities))
}

func (*bleveLexicalCandidateSearcherProbe) Min() int {
	return 0
}

func (*bleveLexicalCandidateSearcherProbe) Size() int {
	return 0
}

func (*bleveLexicalCandidateSearcherProbe) DocumentMatchPoolSize() int {
	return 1
}

type bleveLexicalCandidateIndexReaderProbe struct {
	bleveindex.IndexReader
	identities []bleveindex.IndexInternalID
	readerErr  error
	count      uint64
	countErr   error
	reader     *bleveLexicalCandidateDocIDReaderProbe
}

func (probe *bleveLexicalCandidateIndexReaderProbe) DocIDReaderAll() (
	bleveindex.DocIDReader,
	error,
) {
	if probe.readerErr != nil {
		return nil, probe.readerErr
	}
	probe.reader = &bleveLexicalCandidateDocIDReaderProbe{identities: probe.identities}

	return probe.reader, nil
}

func (probe *bleveLexicalCandidateIndexReaderProbe) DocCount() (uint64, error) {
	return probe.count, probe.countErr
}

type bleveLexicalCandidateDocIDReaderProbe struct {
	identities []bleveindex.IndexInternalID
	position   int
	closed     bool
}

func (probe *bleveLexicalCandidateDocIDReaderProbe) Next() (
	bleveindex.IndexInternalID,
	error,
) {
	if probe.position >= len(probe.identities) {
		return nil, nil
	}
	identity := probe.identities[probe.position]
	probe.position++

	return identity, nil
}

func (probe *bleveLexicalCandidateDocIDReaderProbe) Advance(
	identity bleveindex.IndexInternalID,
) (bleveindex.IndexInternalID, error) {
	for probe.position < len(probe.identities) &&
		probe.identities[probe.position].Compare(identity) < 0 {
		probe.position++
	}

	return probe.Next()
}

func (*bleveLexicalCandidateDocIDReaderProbe) Size() int {
	return 0
}

func (probe *bleveLexicalCandidateDocIDReaderProbe) Close() error {
	probe.closed = true

	return nil
}

func TestCollectBleveLexicalCandidateSnapshotBoundsAndCopies(t *testing.T) {
	third := bleveindex.NewIndexInternalID(nil, 3)
	first := bleveindex.NewIndexInternalID(nil, 1)
	second := bleveindex.NewIndexInternalID(nil, 2)
	searchProbe := newBleveLexicalCandidateSearcherProbe(third, first, second)
	queryProbe := &bleveLexicalCandidateQueryProbe{searcher: searchProbe}
	reader := &bleveLexicalCandidateIndexReaderProbe{}
	identities, complete, err := collectBleveLexicalCandidateSnapshot(
		t.Context(),
		reader,
		mapping.NewIndexMapping(),
		queryProbe,
		3,
	)
	if err != nil || !complete ||
		!slices.EqualFunc(
			identities,
			[]bleveindex.IndexInternalID{first, second, third},
			func(left, right bleveindex.IndexInternalID) bool { return left.Equals(right) },
		) {
		t.Fatalf("identities=%v complete=%t error=%v", identities, complete, err)
	}
	third[0]++
	if identities[2].Equals(third) {
		t.Fatal("candidate identity aliases the collected match")
	}
	if queryProbe.calls != 1 || queryProbe.reader != reader ||
		queryProbe.options.Score != bleve.ScoreNone || queryProbe.options.Explain ||
		queryProbe.options.IncludeTermVectors || !searchProbe.closed {
		t.Fatalf(
			"query calls=%d reader=%p options=%+v closed=%t",
			queryProbe.calls,
			queryProbe.reader,
			queryProbe.options,
			searchProbe.closed,
		)
	}

	emptyProbe := newBleveLexicalCandidateSearcherProbe()
	empty, complete, err := collectBleveLexicalCandidateSnapshot(
		t.Context(),
		reader,
		mapping.NewIndexMapping(),
		&bleveLexicalCandidateQueryProbe{searcher: emptyProbe},
		1,
	)
	if err != nil || !complete || len(empty) != 0 || !emptyProbe.closed {
		t.Fatalf("empty=%v complete=%t error=%v closed=%t", empty, complete, err, emptyProbe.closed)
	}

	overflowProbe := newBleveLexicalCandidateSearcherProbe(first, second, third)
	overflow, complete, err := collectBleveLexicalCandidateSnapshot(
		t.Context(),
		reader,
		mapping.NewIndexMapping(),
		&bleveLexicalCandidateQueryProbe{searcher: overflowProbe},
		2,
	)
	if err != nil || complete || overflow != nil || !overflowProbe.closed {
		t.Fatalf(
			"overflow=%v complete=%t error=%v closed=%t",
			overflow,
			complete,
			err,
			overflowProbe.closed,
		)
	}
}

func TestCollectBleveLexicalCandidateSnapshotRejectsInvalidInputs(t *testing.T) {
	reader := &bleveLexicalCandidateIndexReaderProbe{}
	if _, _, err := collectBleveLexicalCandidateSnapshot(
		t.Context(), reader, mapping.NewIndexMapping(), nil, 1,
	); err == nil {
		t.Fatal("nil candidate query accepted")
	}
	if _, _, err := collectBleveLexicalCandidateSnapshot(
		t.Context(), reader, mapping.NewIndexMapping(), bleve.NewMatchAllQuery(), 0,
	); err == nil {
		t.Fatal("zero candidate maximum accepted")
	}

	cause := errors.New("request budget elapsed")
	canceled, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	queryProbe := &bleveLexicalCandidateQueryProbe{}
	if _, _, err := collectBleveLexicalCandidateSnapshot(
		canceled, reader, mapping.NewIndexMapping(), queryProbe, 1,
	); !errors.Is(err, cause) || queryProbe.calls != 0 {
		t.Fatalf("pre-canceled error=%v calls=%d", err, queryProbe.calls)
	}
}

func TestCollectBleveLexicalCandidateSnapshotReturnsFailures(t *testing.T) {
	reader := &bleveLexicalCandidateIndexReaderProbe{}
	sentinel := errors.New("candidate failed")
	queryProbe := &bleveLexicalCandidateQueryProbe{err: sentinel}
	if _, _, err := collectBleveLexicalCandidateSnapshot(
		t.Context(), reader, mapping.NewIndexMapping(), queryProbe, 1,
	); !errors.Is(err, sentinel) {
		t.Fatalf("query error=%v", err)
	}
	queryProbe = &bleveLexicalCandidateQueryProbe{}
	if _, _, err := collectBleveLexicalCandidateSnapshot(
		t.Context(), reader, mapping.NewIndexMapping(), queryProbe, 1,
	); err == nil {
		t.Fatal("nil candidate searcher accepted")
	}

	nextProbe := newBleveLexicalCandidateSearcherProbe()
	nextProbe.nextErrorAt = 0
	nextProbe.nextErr = sentinel
	closeSentinel := errors.New("candidate close failed")
	nextProbe.closeErr = closeSentinel
	if _, _, err := collectBleveLexicalCandidateSnapshot(
		t.Context(),
		reader,
		mapping.NewIndexMapping(),
		&bleveLexicalCandidateQueryProbe{searcher: nextProbe},
		1,
	); !errors.Is(err, sentinel) || !errors.Is(err, closeSentinel) || !nextProbe.closed {
		t.Fatalf("next error=%v closed=%t", err, nextProbe.closed)
	}

	missingProbe := newBleveLexicalCandidateSearcherProbe(nil)
	if _, _, err := collectBleveLexicalCandidateSnapshot(
		t.Context(),
		reader,
		mapping.NewIndexMapping(),
		&bleveLexicalCandidateQueryProbe{searcher: missingProbe},
		1,
	); err == nil || !missingProbe.closed {
		t.Fatalf("missing identity error=%v closed=%t", err, missingProbe.closed)
	}

	closeProbe := newBleveLexicalCandidateSearcherProbe()
	closeProbe.closeErr = sentinel
	if _, _, err := collectBleveLexicalCandidateSnapshot(
		t.Context(),
		reader,
		mapping.NewIndexMapping(),
		&bleveLexicalCandidateQueryProbe{searcher: closeProbe},
		1,
	); !errors.Is(err, sentinel) {
		t.Fatalf("close error=%v", err)
	}

	cause := errors.New("request budget elapsed")
	midContext, midCancel := context.WithCancelCause(t.Context())
	midProbe := newBleveLexicalCandidateSearcherProbe(
		bleveindex.NewIndexInternalID(nil, 1),
		bleveindex.NewIndexInternalID(nil, 2),
	)
	midProbe.cancel = midCancel
	midProbe.cancelAfter = 1
	midProbe.cancelCause = cause
	if _, _, err := collectBleveLexicalCandidateSnapshot(
		midContext,
		reader,
		mapping.NewIndexMapping(),
		&bleveLexicalCandidateQueryProbe{searcher: midProbe},
		2,
	); !errors.Is(err, cause) || !midProbe.closed {
		t.Fatalf("mid-canceled error=%v closed=%t", err, midProbe.closed)
	}
}

func TestBleveLexicalCandidateSnapshotQuerySelectsInternalIdentities(t *testing.T) {
	first := bleveindex.NewIndexInternalID(nil, 1)
	third := bleveindex.NewIndexInternalID(nil, 3)
	queryProbe := &bleveLexicalCandidateQueryProbe{
		searcher: newBleveLexicalCandidateSearcherProbe(third, first),
	}
	query := &bleveLexicalCandidateSnapshotQuery{
		candidate:        queryProbe,
		maximumDocuments: 2,
	}
	reader := &bleveLexicalCandidateIndexReaderProbe{}
	selected, err := query.Searcher(
		t.Context(),
		reader,
		mapping.NewIndexMapping(),
		search.SearcherOptions{Explain: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Count() != 2 || selected.Weight() != 0 || selected.Min() != 0 ||
		selected.DocumentMatchPoolSize() != 1 || selected.Size() <= 0 {
		t.Fatalf(
			"selected count=%d weight=%v min=%d pool=%d size=%d",
			selected.Count(),
			selected.Weight(),
			selected.Min(),
			selected.DocumentMatchPoolSize(),
			selected.Size(),
		)
	}
	selected.SetQueryNorm(2)
	matchContext := &search.SearchContext{
		DocumentMatchPool: search.NewDocumentMatchPool(selected.DocumentMatchPoolSize(), 0),
		IndexReader:       reader,
	}
	firstMatch, err := selected.Next(matchContext)
	if err != nil || firstMatch == nil || !firstMatch.IndexInternalID.Equals(first) ||
		firstMatch.Score != 0 {
		t.Fatalf("first match=%+v error=%v", firstMatch, err)
	}
	matchContext.DocumentMatchPool.Put(firstMatch)
	thirdMatch, err := selected.Advance(
		matchContext,
		bleveindex.NewIndexInternalID(nil, 2),
	)
	if err != nil || thirdMatch == nil || !thirdMatch.IndexInternalID.Equals(third) ||
		thirdMatch.Score != 0 {
		t.Fatalf("third match=%+v error=%v", thirdMatch, err)
	}
	matchContext.DocumentMatchPool.Put(thirdMatch)
	end, err := selected.Next(matchContext)
	if err != nil || end != nil {
		t.Fatalf("end=%+v error=%v", end, err)
	}
	if err := selected.Close(); err != nil {
		t.Fatal(err)
	}

	query.SetField("ignored")
	if query.Field() != "_id" {
		t.Fatalf("candidate field=%q", query.Field())
	}
	fields, err := blevequery.ExtractFields(query, mapping.NewIndexMapping(), nil)
	if err != nil || !fields.HasID() {
		t.Fatalf("candidate fields=%v error=%v", fields, err)
	}
	if queryProbe.reader != reader || queryProbe.options.Score != bleve.ScoreNone {
		t.Fatalf("candidate reader=%p options=%+v", queryProbe.reader, queryProbe.options)
	}
}

func TestBleveLexicalCandidateSnapshotQueryReturnsEmptyAndOverflowFilters(t *testing.T) {
	reader := &bleveLexicalCandidateIndexReaderProbe{
		identities: []bleveindex.IndexInternalID{
			bleveindex.NewIndexInternalID(nil, 1),
			bleveindex.NewIndexInternalID(nil, 2),
			bleveindex.NewIndexInternalID(nil, 3),
		},
		count: 3,
	}
	empty := &bleveLexicalCandidateSnapshotQuery{
		candidate: &bleveLexicalCandidateQueryProbe{
			searcher: newBleveLexicalCandidateSearcherProbe(),
		},
		maximumDocuments: 1,
	}
	emptyFilter, err := empty.Searcher(
		t.Context(), reader, mapping.NewIndexMapping(), search.SearcherOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := emptyFilter.(*bleveLexicalCandidateInternalSearcher); !ok ||
		emptyFilter.Count() != 0 {
		t.Fatalf("empty filter=%T count=%d", emptyFilter, emptyFilter.Count())
	}
	if err := emptyFilter.Close(); err != nil {
		t.Fatal(err)
	}

	overflow := &bleveLexicalCandidateSnapshotQuery{
		candidate: &bleveLexicalCandidateQueryProbe{searcher: newBleveLexicalCandidateSearcherProbe(
			bleveindex.NewIndexInternalID(nil, 1),
			bleveindex.NewIndexInternalID(nil, 2),
		)},
		maximumDocuments: 1,
	}
	allFilter, err := overflow.Searcher(
		t.Context(), reader, mapping.NewIndexMapping(), search.SearcherOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := allFilter.(*searcher.MatchAllSearcher); !ok {
		t.Fatalf("overflow filter=%T", allFilter)
	}
	allFilter.SetQueryNorm(1)
	matchContext := &search.SearchContext{
		DocumentMatchPool: search.NewDocumentMatchPool(allFilter.DocumentMatchPoolSize(), 0),
		IndexReader:       reader,
	}
	for position, identity := range reader.identities {
		match, err := allFilter.Next(matchContext)
		if err != nil || match == nil || !match.IndexInternalID.Equals(identity) ||
			match.Score != 0 {
			t.Fatalf("match %d=%+v error=%v", position, match, err)
		}
		matchContext.DocumentMatchPool.Put(match)
	}
	end, err := allFilter.Next(matchContext)
	if err != nil || end != nil {
		t.Fatalf("overflow end=%+v error=%v", end, err)
	}
	if err := allFilter.Close(); err != nil || reader.reader == nil || !reader.reader.closed {
		t.Fatalf("overflow close=%v reader=%+v", err, reader.reader)
	}
}

func TestBleveLexicalCandidateSnapshotQueryReturnsOverflowReaderFailures(t *testing.T) {
	sentinel := errors.New("all documents failed")
	failedCandidate := &bleveLexicalCandidateSnapshotQuery{
		candidate:        &bleveLexicalCandidateQueryProbe{err: sentinel},
		maximumDocuments: 1,
	}
	if _, err := failedCandidate.Searcher(
		t.Context(),
		&bleveLexicalCandidateIndexReaderProbe{},
		mapping.NewIndexMapping(),
		search.SearcherOptions{},
	); !errors.Is(err, sentinel) {
		t.Fatalf("candidate error=%v", err)
	}

	query := &bleveLexicalCandidateSnapshotQuery{
		candidate: &bleveLexicalCandidateQueryProbe{searcher: newBleveLexicalCandidateSearcherProbe(
			bleveindex.NewIndexInternalID(nil, 1),
			bleveindex.NewIndexInternalID(nil, 2),
		)},
		maximumDocuments: 1,
	}
	reader := &bleveLexicalCandidateIndexReaderProbe{readerErr: sentinel}
	if _, err := query.Searcher(
		t.Context(), reader, mapping.NewIndexMapping(), search.SearcherOptions{},
	); !errors.Is(err, sentinel) {
		t.Fatalf("reader error=%v", err)
	}

	reader.readerErr = nil
	reader.countErr = sentinel
	query.candidate = &bleveLexicalCandidateQueryProbe{
		searcher: newBleveLexicalCandidateSearcherProbe(
			bleveindex.NewIndexInternalID(nil, 1),
			bleveindex.NewIndexInternalID(nil, 2),
		),
	}
	if _, err := query.Searcher(
		t.Context(), reader, mapping.NewIndexMapping(), search.SearcherOptions{},
	); !errors.Is(err, sentinel) || reader.reader == nil || !reader.reader.closed {
		t.Fatalf("count error=%v reader=%+v", err, reader.reader)
	}
}

func TestNewBleveLexicalCandidateSnapshotQueryUsesPerShardBound(t *testing.T) {
	query, ok := newBleveLexicalCandidateSnapshotQuery(
		SearchRequest{Query: "needle"},
		true,
	).(*bleveLexicalCandidateSnapshotQuery)
	if !ok || query.candidate == nil ||
		query.maximumDocuments != diskLexicalCandidateMaximumDocumentsPerShard ||
		diskLexicalCandidateMaximumDocumentsPerShard*diskShardCount !=
			diskLexicalCandidateMaximumDocuments {
		t.Fatalf("candidate query=%#v", query)
	}
}

var (
	_ blevequery.Query       = (*bleveLexicalCandidateQueryProbe)(nil)
	_ search.Searcher        = (*bleveLexicalCandidateSearcherProbe)(nil)
	_ bleveindex.DocIDReader = (*bleveLexicalCandidateDocIDReaderProbe)(nil)
)
