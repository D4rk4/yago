package searchindex

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	bleveindex "github.com/blevesearch/bleve_index_api"
	segment "github.com/blevesearch/scorch_segment_api/v2"
)

type bleveSegmentDictionariesProbe struct {
	inner              bleveSegmentTermDictionaries
	dictionaryFailure  error
	lookupFailures     map[int]error
	dictionariesOpened int
	lookups            int
}

func (probe *bleveSegmentDictionariesProbe) Dictionary(
	field string,
) (segment.TermDictionary, error) {
	probe.dictionariesOpened++
	if probe.dictionaryFailure != nil {
		return nil, probe.dictionaryFailure
	}
	dictionary, err := probe.inner.Dictionary(field)
	if err != nil {
		return nil, fmt.Errorf("probe term dictionary: %w", err)
	}

	return bleveTermDictionaryProbe{TermDictionary: dictionary, segmentProbe: probe}, nil
}

type bleveTermDictionaryProbe struct {
	segment.TermDictionary
	segmentProbe *bleveSegmentDictionariesProbe
}

func (probe bleveTermDictionaryProbe) PostingsList(
	term []byte,
	except *roaring.Bitmap,
	preAllocated segment.PostingsList,
) (segment.PostingsList, error) {
	probe.segmentProbe.lookups++
	if failure := probe.segmentProbe.lookupFailures[probe.segmentProbe.lookups]; failure != nil {
		return nil, failure
	}

	list, err := probe.TermDictionary.PostingsList(term, except, preAllocated)
	if err != nil {
		return nil, fmt.Errorf("probe term postings: %w", err)
	}

	return list, nil
}

type bleveSegmentReads struct {
	DictionariesOpened int
	Lookups            int
}

func probedBleveSearchSnapshot(
	t *testing.T,
	snapshot *scorch.IndexSnapshot,
) (*bleveSearchSnapshot, []*bleveSegmentDictionariesProbe) {
	t.Helper()
	searchSnapshot, ok := withBleveSearchSnapshot(snapshot).(*bleveSearchSnapshot)
	if !ok {
		t.Fatal("scorch snapshot was not wrapped")
	}
	probes := make([]*bleveSegmentDictionariesProbe, 0, len(searchSnapshot.segments))
	for position := range searchSnapshot.segments {
		probe := &bleveSegmentDictionariesProbe{
			inner: searchSnapshot.segments[position].dictionaries,
		}
		searchSnapshot.segments[position].dictionaries = probe
		probes = append(probes, probe)
	}

	return searchSnapshot, probes
}

func bleveSegmentProbeReads(probes []*bleveSegmentDictionariesProbe) []bleveSegmentReads {
	reads := make([]bleveSegmentReads, 0, len(probes))
	for _, probe := range probes {
		reads = append(reads, bleveSegmentReads{
			DictionariesOpened: probe.dictionariesOpened,
			Lookups:            probe.lookups,
		})
	}

	return reads
}

func TestBleveSearchSnapshotLeavesOtherIndexReadersUnchanged(t *testing.T) {
	reader := &bleveSearchDeadlineIndexReaderProbe{}
	if wrapped := withBleveSearchSnapshot(reader); wrapped != bleveindex.IndexReader(reader) {
		t.Fatalf("wrapped reader = %T, want the original reader", wrapped)
	}
}

func TestBleveSearchDeadlineReaderSharesTermPostingsWithinSearch(t *testing.T) {
	reader := newBleveSearchDeadlineReader(t.Context(), newBleveTermPostingsFixture(t))
	bounded, ok := reader.(bleveSearchDeadlineAutomatonBM25Reader)
	if !ok {
		t.Fatalf("deadline reader = %T, want fuzzy and BM25 capabilities", reader)
	}
	if _, shared := bounded.IndexReader.(*bleveSearchSnapshot); !shared {
		t.Fatalf("deadline reader reads through %T, want shared term postings",
			bounded.IndexReader)
	}
}

func TestBleveSearchSnapshotReadsEachFieldTermOncePerSearch(t *testing.T) {
	searchSnapshot, probes := probedBleveSearchSnapshot(t, newBleveTermPostingsFixture(t))
	alpha := bleveFieldTerm{field: "body", term: "alpha"}
	openBleveTermPostingsReader(t, searchSnapshot, alpha, bleveTermPostingsDetail{})
	afterFirstOpen := bleveSegmentProbeReads(probes)
	for _, read := range afterFirstOpen {
		if read.DictionariesOpened != 1 || read.Lookups == 0 {
			t.Fatalf("first open reads = %+v, want one dictionary and a lookup", afterFirstOpen)
		}
	}
	for _, detail := range []bleveTermPostingsDetail{
		{frequency: true, norm: true},
		{frequency: true, norm: true, termVectors: true},
	} {
		openBleveTermPostingsReader(t, searchSnapshot, alpha, detail)
	}
	if reads := bleveSegmentProbeReads(probes); !reflect.DeepEqual(reads, afterFirstOpen) {
		t.Fatalf("repeated opens read %+v, want %+v", reads, afterFirstOpen)
	}
	openBleveTermPostingsReader(
		t,
		searchSnapshot,
		bleveFieldTerm{field: "body", term: "beta"},
		bleveTermPostingsDetail{},
	)
	openBleveTermPostingsReader(
		t,
		searchSnapshot,
		bleveFieldTerm{field: "tag", term: "alpha"},
		bleveTermPostingsDetail{},
	)
	for position, read := range bleveSegmentProbeReads(probes) {
		if read.DictionariesOpened != 2 {
			t.Fatalf(
				"segment %d dictionaries opened = %d, want 2",
				position,
				read.DictionariesOpened,
			)
		}
		if read.Lookups < afterFirstOpen[position].Lookups+2 {
			t.Fatalf("segment %d lookups = %d, want new lookups for each new field term",
				position, read.Lookups)
		}
	}
}

func TestBleveSearchSnapshotReportsUnreadableTermDictionary(t *testing.T) {
	searchSnapshot, probes := probedBleveSearchSnapshot(t, newBleveTermPostingsFixture(t))
	failure := errors.New("dictionary unreadable")
	probes[len(probes)-1].dictionaryFailure = failure
	alpha := bleveFieldTerm{field: "body", term: "alpha"}
	_, err := searchSnapshot.TermFieldReader(t.Context(), []byte(alpha.term), alpha.field,
		false, false, false)
	if !errors.Is(err, failure) {
		t.Fatalf("TermFieldReader error = %v, want %v", err, failure)
	}
	probes[len(probes)-1].dictionaryFailure = nil
	reader := openBleveTermPostingsReader(t, searchSnapshot, alpha, bleveTermPostingsDetail{})
	if reader.Count() == 0 {
		t.Fatal("recovered dictionary read no postings")
	}
}

func TestBleveSearchSnapshotReportsUnreadableTermPostings(t *testing.T) {
	for _, failedLookup := range []int{1, 2} {
		t.Run(fmt.Sprintf("lookup=%d", failedLookup), func(t *testing.T) {
			searchSnapshot, probes := probedBleveSearchSnapshot(t, newBleveTermPostingsFixture(t))
			failure := errors.New("postings unreadable")
			for _, probe := range probes {
				probe.lookupFailures = map[int]error{failedLookup: failure}
			}
			_, err := searchSnapshot.TermFieldReader(t.Context(), []byte("gone"), "tag",
				false, false, false)
			if !errors.Is(err, failure) {
				t.Fatalf("TermFieldReader error = %v, want %v", err, failure)
			}
		})
	}
}
