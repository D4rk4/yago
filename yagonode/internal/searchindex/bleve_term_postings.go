package searchindex

import (
	"fmt"

	"github.com/RoaringBitmap/roaring/v2"
	segment "github.com/blevesearch/scorch_segment_api/v2"
)

type bleveTermSegmentPostings struct {
	list          segment.PostingsList
	liveDocuments *roaring.Bitmap
}

type bleveTermPostings struct {
	segments          []bleveTermSegmentPostings
	documentFrequency uint64
}

type bleveTermPostingsDetail struct {
	frequency   bool
	norm        bool
	termVectors bool
}

func readBleveTermPostings(
	segments []bleveSearchSegment,
	dictionaries []segment.TermDictionary,
	term []byte,
) (bleveTermPostings, error) {
	postings := bleveTermPostings{segments: make([]bleveTermSegmentPostings, len(segments))}
	for position, searchSegment := range segments {
		segmentPostings, documentFrequency, err := readBleveTermSegmentPostings(
			dictionaries[position],
			term,
			searchSegment.deleted,
		)
		if err != nil {
			return bleveTermPostings{}, err
		}
		postings.segments[position] = segmentPostings
		postings.documentFrequency += documentFrequency
	}

	return postings, nil
}

func readBleveTermSegmentPostings(
	dictionary segment.TermDictionary,
	term []byte,
	deleted *roaring.Bitmap,
) (bleveTermSegmentPostings, uint64, error) {
	list, err := dictionary.PostingsList(term, nil, nil)
	if err != nil {
		return bleveTermSegmentPostings{}, 0, fmt.Errorf("read term postings: %w", err)
	}
	if deleted == nil || list.Count() == 0 {
		return bleveTermSegmentPostings{list: list}, list.Count(), nil
	}
	probe, optimizable := list.Iterator(false, false, false, nil).(segment.OptimizablePostingsIterator)
	if optimizable && probe.ActualBitmap() != nil {
		liveDocuments := roaring.AndNot(probe.ActualBitmap(), deleted)

		return bleveTermSegmentPostings{list: list, liveDocuments: liveDocuments},
			liveDocuments.GetCardinality(),
			nil
	}
	list, err = dictionary.PostingsList(term, deleted, nil)
	if err != nil {
		return bleveTermSegmentPostings{}, 0, fmt.Errorf("read live term postings: %w", err)
	}

	return bleveTermSegmentPostings{list: list}, list.Count(), nil
}

func (postings bleveTermSegmentPostings) iterator(
	detail bleveTermPostingsDetail,
) segment.PostingsIterator {
	iterator := postings.list.Iterator(detail.frequency, detail.norm, detail.termVectors, nil)
	live, optimizable := iterator.(segment.OptimizablePostingsIterator)
	if optimizable && postings.liveDocuments != nil {
		live.ReplaceActual(postings.liveDocuments)
	}

	return iterator
}
