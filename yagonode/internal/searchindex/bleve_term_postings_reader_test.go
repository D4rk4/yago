package searchindex

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	"github.com/blevesearch/bleve/v2/mapping"
	bleveindex "github.com/blevesearch/bleve_index_api"
	segment "github.com/blevesearch/scorch_segment_api/v2"
)

type bleveTermPostingsFixtureDocument struct {
	Body string `json:"body"`
	Tag  string `json:"tag"`
}

type bleveTermPostingsStep struct {
	advanceTo uint64
	advance   bool
}

type bleveTermPostingsObservation struct {
	Document  uint64
	Frequency uint64
	Norm      float64
	Vectors   []bleveindex.TermFieldVector
	Exhausted bool
}

func newBleveTermPostingsFixture(t *testing.T) *scorch.IndexSnapshot {
	t.Helper()
	documentMapping := mapping.NewDocumentMapping()
	documentMapping.AddFieldMappingsAt("body", mapping.NewTextFieldMapping())
	tag := mapping.NewKeywordFieldMapping()
	tag.IncludeTermVectors = false
	documentMapping.AddFieldMappingsAt("tag", tag)
	indexMapping := mapping.NewIndexMapping()
	indexMapping.DefaultMapping = documentMapping
	index := newBleveTermPostingsShard(t, indexMapping)
	indexBleveTermPostingsBatch(t, index, map[string]any{
		"d00": bleveTermPostingsFixtureDocument{Body: "alpha beta alpha", Tag: "shared"},
		"d01": bleveTermPostingsFixtureDocument{Body: "alpha gamma", Tag: "gone"},
		"d02": bleveTermPostingsFixtureDocument{Body: "beta gamma beta beta", Tag: "shared"},
		"d03": bleveTermPostingsFixtureDocument{Body: "alpha delta", Tag: "shared"},
		"d04": bleveTermPostingsFixtureDocument{Body: "gamma", Tag: "first"},
	})
	indexBleveTermPostingsBatch(t, index, map[string]any{
		"d05": bleveTermPostingsFixtureDocument{Body: "alpha alpha alpha beta", Tag: "kept"},
		"d06": bleveTermPostingsFixtureDocument{Body: "beta delta", Tag: "shared"},
		"d07": bleveTermPostingsFixtureDocument{Body: "alpha gamma delta", Tag: "shared"},
		"d08": bleveTermPostingsFixtureDocument{Body: "delta delta", Tag: "shared"},
	})
	forceSingleScorchSegment(t, index)
	deleteBleveTermPostingsDocuments(t, index, "d01", "d07")
	indexBleveTermPostingsBatch(t, index, map[string]any{
		"d03": bleveTermPostingsFixtureDocument{Body: "beta beta", Tag: "moved"},
		"d09": bleveTermPostingsFixtureDocument{Body: "alpha beta gamma delta", Tag: "shared"},
	})
	indexBleveTermPostingsBatch(t, index, map[string]any{
		"d09": bleveTermPostingsFixtureDocument{Body: "gamma alpha beta", Tag: "shared"},
		"d10": bleveTermPostingsFixtureDocument{Body: "gamma alpha", Tag: "last"},
		"d11": bleveTermPostingsFixtureDocument{Body: "alpha", Tag: "shared"},
	})

	return bleveTermPostingsShardSnapshot(t, index)
}

func newBleveTermPostingsShard(t *testing.T, indexMapping mapping.IndexMapping) bleve.Index {
	t.Helper()
	configuration := bleveDiskScorchConfiguration()
	configuration["scorchMergePlanOptions"] = map[string]interface{}{
		"MaxSegmentsPerTier":   1000,
		"FloorSegmentSize":     1,
		"FloorSegmentFileSize": 1,
	}
	index, err := bleve.NewUsing(
		filepath.Join(t.TempDir(), "postings.idx"),
		indexMapping,
		scorch.Name,
		scorch.Name,
		configuration,
	)
	if err != nil {
		t.Fatalf("open fixture index: %v", err)
	}
	t.Cleanup(func() { _ = index.Close() })

	return index
}

func bleveTermPostingsShardSnapshot(t *testing.T, index bleve.Index) *scorch.IndexSnapshot {
	t.Helper()
	advanced, err := index.Advanced()
	if err != nil {
		t.Fatalf("advanced fixture index: %v", err)
	}
	reader, err := advanced.Reader()
	if err != nil {
		t.Fatalf("fixture reader: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	snapshot, ok := reader.(*scorch.IndexSnapshot)
	if !ok {
		t.Fatalf("fixture reader = %T, want scorch snapshot", reader)
	}

	return snapshot
}

func deleteBleveTermPostingsDocuments(t *testing.T, index bleve.Index, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := index.Delete(id); err != nil {
			t.Fatalf("delete fixture document: %v", err)
		}
	}
}

func indexBleveTermPostingsBatch(
	t *testing.T,
	index bleve.Index,
	documents map[string]any,
) {
	t.Helper()
	batch := index.NewBatch()
	for id, document := range documents {
		if err := batch.Index(id, document); err != nil {
			t.Fatalf("batch fixture document: %v", err)
		}
	}
	if err := index.Batch(batch); err != nil {
		t.Fatalf("index fixture batch: %v", err)
	}
}

func requireBleveTermPostingsFixtureShape(t *testing.T, snapshot *scorch.IndexSnapshot) {
	t.Helper()
	deletedSegments := 0
	for _, segmentSnapshot := range snapshot.Segments() {
		if segmentSnapshot.Deleted() != nil {
			deletedSegments++
		}
	}
	if deletedSegments == 0 || deletedSegments == len(snapshot.Segments()) {
		t.Fatalf("fixture has %d segments, %d with deletions; want several of each kind",
			len(snapshot.Segments()), deletedSegments)
	}
	for _, tag := range []string{"gone", "kept", "first"} {
		if !bleveTermPostingsFixtureHasSingleHit(t, snapshot, tag) {
			t.Fatalf("fixture tag %q is not single-hit encoded", tag)
		}
	}
}

func bleveTermPostingsFixtureHasSingleHit(
	t *testing.T,
	snapshot *scorch.IndexSnapshot,
	tag string,
) bool {
	t.Helper()
	for _, segmentSnapshot := range snapshot.Segments() {
		dictionary, err := segmentSnapshot.Segment().Dictionary("tag")
		if err != nil {
			t.Fatalf("fixture dictionary: %v", err)
		}
		list, err := dictionary.PostingsList([]byte(tag), nil, nil)
		if err != nil {
			t.Fatalf("fixture postings: %v", err)
		}
		probe, ok := list.Iterator(false, false, false, nil).(segment.OptimizablePostingsIterator)
		if !ok {
			continue
		}
		if _, single := probe.DocNum1Hit(); single {
			return true
		}
	}

	return false
}

func bleveTermPostingsScript() []bleveTermPostingsStep {
	return []bleveTermPostingsStep{
		{},
		{},
		{advance: true, advanceTo: 6},
		{},
		{advance: true, advanceTo: 2},
		{advance: true, advanceTo: 2},
		{advance: true, advanceTo: 9},
		{},
		{advance: true, advanceTo: 4},
		{},
		{},
		{advance: true, advanceTo: 40},
		{},
		{advance: true, advanceTo: 0},
		{},
		{},
		{},
		{},
		{},
		{},
		{},
		{},
		{},
		{},
		{},
		{},
	}
}

func observeBleveTermPostings(
	t *testing.T,
	reader bleveindex.TermFieldReader,
	reuse bool,
) []bleveTermPostingsObservation {
	t.Helper()
	var preAllocated *bleveindex.TermFieldDoc
	if reuse {
		preAllocated = &bleveindex.TermFieldDoc{}
	}
	observations := make([]bleveTermPostingsObservation, 0, len(bleveTermPostingsScript()))
	for _, step := range bleveTermPostingsScript() {
		var document *bleveindex.TermFieldDoc
		var err error
		if step.advance {
			document, err = reader.Advance(
				bleveindex.NewIndexInternalID(nil, step.advanceTo),
				preAllocated,
			)
		} else {
			document, err = reader.Next(preAllocated)
		}
		if err != nil {
			t.Fatalf("read term postings: %v", err)
		}
		observations = append(observations, bleveTermPostingsObserved(document))
	}

	return observations
}

func bleveTermPostingsObserved(document *bleveindex.TermFieldDoc) bleveTermPostingsObservation {
	if document == nil {
		return bleveTermPostingsObservation{Exhausted: true}
	}
	observation := bleveTermPostingsObservation{
		Document:  document.ID.Value(),
		Frequency: document.Freq,
		Norm:      document.Norm,
	}
	for _, vector := range document.Vectors {
		observation.Vectors = append(observation.Vectors, *vector)
	}

	return observation
}

func TestBleveTermPostingsReaderMatchesScorchTermReader(t *testing.T) {
	snapshot := newBleveTermPostingsFixture(t)
	requireBleveTermPostingsFixtureShape(t, snapshot)
	searchSnapshot, ok := withBleveSearchSnapshot(snapshot).(*bleveSearchSnapshot)
	if !ok {
		t.Fatal("scorch snapshot was not wrapped")
	}
	fieldTerms := []bleveFieldTerm{
		{field: "body", term: "alpha"},
		{field: "body", term: "beta"},
		{field: "body", term: "gamma"},
		{field: "body", term: "delta"},
		{field: "body", term: "absent"},
		{field: "tag", term: "shared"},
		{field: "tag", term: "gone"},
		{field: "tag", term: "kept"},
		{field: "tag", term: "first"},
		{field: "tag", term: "last"},
		{field: "tag", term: "moved"},
		{field: "missing", term: "alpha"},
	}
	details := []bleveTermPostingsDetail{
		{},
		{frequency: true},
		{frequency: true, norm: true},
		{frequency: true, norm: true, termVectors: true},
		{termVectors: true},
	}
	for _, fieldTerm := range fieldTerms {
		for _, detail := range details {
			for _, reuse := range []bool{false, true} {
				reading := bleveTermPostingsReading{
					fieldTerm: fieldTerm,
					detail:    detail,
					reuse:     reuse,
				}
				t.Run(fmt.Sprintf("%+v", reading), func(t *testing.T) {
					compareBleveTermPostingsReaders(t, snapshot, searchSnapshot, reading)
				})
			}
		}
	}
}

type bleveTermPostingsReading struct {
	fieldTerm bleveFieldTerm
	detail    bleveTermPostingsDetail
	reuse     bool
}

func compareBleveTermPostingsReaders(
	t *testing.T,
	snapshot *scorch.IndexSnapshot,
	searchSnapshot *bleveSearchSnapshot,
	reading bleveTermPostingsReading,
) {
	t.Helper()
	want := openBleveTermPostingsReader(t, snapshot, reading.fieldTerm, reading.detail)
	got := openBleveTermPostingsReader(t, searchSnapshot, reading.fieldTerm, reading.detail)
	if got.Count() != want.Count() {
		t.Fatalf("Count = %d, want %d", got.Count(), want.Count())
	}
	wantObservations := observeBleveTermPostings(t, want, reading.reuse)
	gotObservations := observeBleveTermPostings(t, got, reading.reuse)
	if !reflect.DeepEqual(gotObservations, wantObservations) {
		t.Fatalf("observations = %+v\nwant %+v", gotObservations, wantObservations)
	}
	if got.Size() <= 0 {
		t.Fatalf("Size = %d, want positive", got.Size())
	}
	if err := got.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	_ = want.Close()
}

func openBleveTermPostingsReader(
	t *testing.T,
	reader bleveindex.IndexReader,
	fieldTerm bleveFieldTerm,
	detail bleveTermPostingsDetail,
) bleveindex.TermFieldReader {
	t.Helper()
	opened, err := reader.TermFieldReader(
		t.Context(),
		[]byte(fieldTerm.term),
		fieldTerm.field,
		detail.frequency,
		detail.norm,
		detail.termVectors,
	)
	if err != nil {
		t.Fatalf("open term reader: %v", err)
	}

	return opened
}

type bleveFailingPostingsList struct {
	segment.PostingsList
	failure error
}

func (list bleveFailingPostingsList) Iterator(
	_ bool,
	_ bool,
	_ bool,
	_ segment.PostingsIterator,
) segment.PostingsIterator {
	return bleveFailingPostingsIterator{failure: list.failure}
}

type bleveFailingPostingsIterator struct {
	segment.PostingsIterator
	failure error
}

func (iterator bleveFailingPostingsIterator) Next() (segment.Posting, error) {
	return nil, iterator.failure
}

func (iterator bleveFailingPostingsIterator) Advance(uint64) (segment.Posting, error) {
	return nil, iterator.failure
}

func TestBleveTermPostingsReaderReportsUnreadablePostings(t *testing.T) {
	failure := errors.New("posting unreadable")
	reader := newBleveTermPostingsReader(
		[]bleveSearchSegment{{}},
		bleveTermPostings{segments: []bleveTermSegmentPostings{
			{list: bleveFailingPostingsList{failure: failure}},
		}},
		bleveTermPostingsDetail{},
	)
	if _, err := reader.Next(nil); !errors.Is(err, failure) {
		t.Fatalf("Next error = %v, want %v", err, failure)
	}
	_, err := reader.Advance(bleveindex.NewIndexInternalID(nil, 0), nil)
	if !errors.Is(err, failure) {
		t.Fatalf("Advance error = %v, want %v", err, failure)
	}
}
