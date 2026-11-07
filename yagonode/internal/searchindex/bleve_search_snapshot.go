package searchindex

import (
	"context"
	"fmt"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/blevesearch/bleve/v2/index/scorch"
	bleveindex "github.com/blevesearch/bleve_index_api"
	segment "github.com/blevesearch/scorch_segment_api/v2"
)

type bleveSegmentTermDictionaries interface {
	Dictionary(field string) (segment.TermDictionary, error)
}

type bleveSearchSegment struct {
	dictionaries        bleveSegmentTermDictionaries
	deleted             *roaring.Bitmap
	firstDocumentNumber uint64
}

type bleveFieldTerm struct {
	field string
	term  string
}

type bleveSearchSnapshot struct {
	*scorch.IndexSnapshot
	segments     []bleveSearchSegment
	dictionaries map[string][]segment.TermDictionary
	postings     map[bleveFieldTerm]bleveTermPostings
}

func withBleveSearchSnapshot(reader bleveindex.IndexReader) bleveindex.IndexReader {
	snapshot, isScorchSnapshot := reader.(*scorch.IndexSnapshot)
	if !isScorchSnapshot {
		return reader
	}
	segments := make([]bleveSearchSegment, 0, len(snapshot.Segments()))
	var firstDocumentNumber uint64
	for _, segmentSnapshot := range snapshot.Segments() {
		segments = append(segments, bleveSearchSegment{
			dictionaries:        segmentSnapshot.Segment(),
			deleted:             segmentSnapshot.Deleted(),
			firstDocumentNumber: firstDocumentNumber,
		})
		firstDocumentNumber += segmentSnapshot.Segment().Count()
	}

	return &bleveSearchSnapshot{
		IndexSnapshot: snapshot,
		segments:      segments,
		dictionaries:  map[string][]segment.TermDictionary{},
		postings:      map[bleveFieldTerm]bleveTermPostings{},
	}
}

func (snapshot *bleveSearchSnapshot) TermFieldReader(
	_ context.Context,
	term []byte,
	field string,
	includeFrequency bool,
	includeNorm bool,
	includeTermVectors bool,
) (bleveindex.TermFieldReader, error) {
	postings, err := snapshot.termPostings(field, term)
	if err != nil {
		return nil, err
	}

	return newBleveTermPostingsReader(snapshot.segments, postings, bleveTermPostingsDetail{
		frequency:   includeFrequency,
		norm:        includeNorm,
		termVectors: includeTermVectors,
	}), nil
}

func (snapshot *bleveSearchSnapshot) termPostings(
	field string,
	term []byte,
) (bleveTermPostings, error) {
	fieldTerm := bleveFieldTerm{field: field, term: string(term)}
	if postings, read := snapshot.postings[fieldTerm]; read {
		return postings, nil
	}
	dictionaries, err := snapshot.fieldDictionaries(field)
	if err != nil {
		return bleveTermPostings{}, err
	}
	postings, err := readBleveTermPostings(snapshot.segments, dictionaries, term)
	if err != nil {
		return bleveTermPostings{}, err
	}
	snapshot.postings[fieldTerm] = postings

	return postings, nil
}

func (snapshot *bleveSearchSnapshot) fieldDictionaries(
	field string,
) ([]segment.TermDictionary, error) {
	if dictionaries, opened := snapshot.dictionaries[field]; opened {
		return dictionaries, nil
	}
	dictionaries := make([]segment.TermDictionary, len(snapshot.segments))
	for position, searchSegment := range snapshot.segments {
		dictionary, err := searchSegment.dictionaries.Dictionary(field)
		if err != nil {
			return nil, fmt.Errorf("open term dictionary: %w", err)
		}
		dictionaries[position] = dictionary
	}
	snapshot.dictionaries[field] = dictionaries

	return dictionaries, nil
}
