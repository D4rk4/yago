package searchindex

import (
	"fmt"
	"sort"

	bleveindex "github.com/blevesearch/bleve_index_api"
	segment "github.com/blevesearch/scorch_segment_api/v2"
)

type bleveTermPostingsReader struct {
	segments        []bleveSearchSegment
	postings        bleveTermPostings
	detail          bleveTermPostingsDetail
	iterators       []segment.PostingsIterator
	segment         int
	currentPosting  segment.Posting
	currentDocument bleveindex.IndexInternalID
}

func newBleveTermPostingsReader(
	segments []bleveSearchSegment,
	postings bleveTermPostings,
	detail bleveTermPostingsDetail,
) *bleveTermPostingsReader {
	reader := &bleveTermPostingsReader{segments: segments, postings: postings, detail: detail}
	reader.restart()

	return reader
}

func (reader *bleveTermPostingsReader) restart() {
	reader.iterators = make([]segment.PostingsIterator, len(reader.postings.segments))
	for position, segmentPostings := range reader.postings.segments {
		reader.iterators[position] = segmentPostings.iterator(reader.detail)
	}
	reader.segment = 0
	reader.currentPosting = nil
	reader.currentDocument = reader.currentDocument[:0]
}

func (reader *bleveTermPostingsReader) Next(
	preAllocated *bleveindex.TermFieldDoc,
) (*bleveindex.TermFieldDoc, error) {
	for ; reader.segment < len(reader.iterators); reader.segment++ {
		posting, err := reader.iterators[reader.segment].Next()
		if err != nil {
			return nil, fmt.Errorf("read next term posting: %w", err)
		}
		if posting != nil {
			return reader.found(posting, preAllocated), nil
		}
	}

	return nil, nil
}

func (reader *bleveTermPostingsReader) Advance(
	document bleveindex.IndexInternalID,
	preAllocated *bleveindex.TermFieldDoc,
) (*bleveindex.TermFieldDoc, error) {
	if reader.currentPosting != nil && reader.currentDocument.Compare(document) >= 0 {
		reader.restart()
	}
	documentNumber := document.Value()
	reader.segment = sort.Search(len(reader.segments), func(position int) bool {
		return reader.segments[position].firstDocumentNumber > documentNumber
	}) - 1
	posting, err := reader.iterators[reader.segment].Advance(
		documentNumber - reader.segments[reader.segment].firstDocumentNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("advance term posting: %w", err)
	}
	if posting == nil {
		return reader.Next(preAllocated)
	}

	return reader.found(posting, preAllocated), nil
}

func (reader *bleveTermPostingsReader) found(
	posting segment.Posting,
	preAllocated *bleveindex.TermFieldDoc,
) *bleveindex.TermFieldDoc {
	document := preAllocated
	if document == nil {
		document = &bleveindex.TermFieldDoc{}
	}
	document.ID = bleveindex.NewIndexInternalID(
		document.ID,
		posting.Number()+reader.segments[reader.segment].firstDocumentNumber,
	)
	reader.describe(posting, document)
	reader.currentDocument = document.ID
	reader.currentPosting = posting

	return document
}

func (reader *bleveTermPostingsReader) describe(
	posting segment.Posting,
	document *bleveindex.TermFieldDoc,
) {
	if reader.detail.frequency {
		document.Freq = posting.Frequency()
	}
	if reader.detail.norm {
		document.Norm = posting.Norm()
	}
	if reader.detail.termVectors {
		describeBleveTermVectors(posting.Locations(), document)
	}
}

func describeBleveTermVectors(locations []segment.Location, document *bleveindex.TermFieldDoc) {
	if cap(document.Vectors) < len(locations) {
		document.Vectors = make([]*bleveindex.TermFieldVector, len(locations))
		backing := make([]bleveindex.TermFieldVector, len(locations))
		for position := range backing {
			document.Vectors[position] = &backing[position]
		}
	}
	document.Vectors = document.Vectors[:len(locations)]
	for position, location := range locations {
		*document.Vectors[position] = bleveindex.TermFieldVector{
			Start:          location.Start(),
			End:            location.End(),
			Pos:            location.Pos(),
			ArrayPositions: location.ArrayPositions(),
			Field:          location.Field(),
		}
	}
}

func (reader *bleveTermPostingsReader) Count() uint64 {
	return reader.postings.documentFrequency
}

func (reader *bleveTermPostingsReader) Size() int {
	size := 0
	for _, iterator := range reader.iterators {
		size += iterator.Size()
	}

	return size
}

func (reader *bleveTermPostingsReader) Close() error {
	return nil
}
